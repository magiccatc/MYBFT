package nodesvc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	goleveldb "github.com/syndtr/goleveldb/leveldb"

	"mybft/internal/common"
	"mybft/internal/crypto"
	"mybft/internal/redisx"
	"mybft/internal/storage"
	leveldbstore "mybft/internal/storage/leveldb"
)

type heightState struct {
	ProposalDigest string
	ProposalTx     []string
	Prepared       map[int]string
	Committed      map[int]string
	Voted          map[int]string
	Dedup          map[string]struct{}
	Done           bool
}

type hotstuffBlock struct {
	Block     common.Block
	QC        *common.QuorumCert
	Committed bool
	Executed  bool
}

type simulatedTx struct {
	From   int
	To     int
	Amount int
	Nonce  int
	Fee    int
}

type Service struct {
	mu               sync.Mutex
	rdb              *redisx.Client
	selfID           int
	alg              string
	cfg              redisx.ClusterConfig
	th               common.Thresholds
	height           int
	view             int
	leaderMode       bool
	keys             map[int]string
	peerAddrs        map[int]string
	clientURL        string
	state            map[int]*heightState
	stores           *leveldbstore.NodeStores
	hotstuffBlocks   map[string]*hotstuffBlock
	hotstuffHighQC   common.QuorumCert
	hotstuffLockedQC common.QuorumCert
	hotstuffVoted    map[int]string
}

// 初始化节点服务：加载集群配置、密钥与同伴地址。
func New(rdb *redisx.Client, selfID int, alg string) (*Service, error) {
	cfg, err := redisx.ReadClusterConfig(rdb)
	if err != nil {
		return nil, err
	}
	dataRoot := os.Getenv("MYBFT_DATA_DIR")
	if dataRoot == "" {
		dataRoot = "data"
	}
	stores, err := leveldbstore.OpenNodeStores(dataRoot, selfID)
	if err != nil {
		return nil, fmt.Errorf("open node stores: %w", err)
	}
	s := &Service{
		rdb:            rdb,
		selfID:         selfID,
		alg:            alg,
		cfg:            cfg,
		th:             common.CalcThresholds(cfg.N),
		height:         1,
		view:           1,
		keys:           map[int]string{},
		peerAddrs:      map[int]string{},
		state:          map[int]*heightState{},
		clientURL:      "http://" + cfg.ClientAddr,
		stores:         stores,
		hotstuffBlocks: map[string]*hotstuffBlock{},
		hotstuffVoted:  map[int]string{},
	}
	for i := 1; i <= cfg.N; i++ {
		sk, err := rdb.HGet(fmt.Sprintf("Node:%d", i), "threshold_sk")
		if err != nil {
			return nil, fmt.Errorf("load key Node:%d: %w", i, err)
		}
		s.keys[i] = sk
		s.peerAddrs[i] = fmt.Sprintf("127.0.0.1:%d", cfg.BasePort+i)
	}
	s.initHotStuffState()
	s.loadPersistedPosition()
	s.loadPersistedHotStuffState()
	s.persistPosition()
	s.leaderMode = s.isLeader(s.view)
	return s, nil
}

// 判断当前节点在指定 view 下是否为 leader。
func (s *Service) isLeader(view int) bool {
	leader := ((view - 1) % s.cfg.N) + 1
	return s.selfID == leader
}

// 若当前为 leader，延迟后触发首轮提案。
func (s *Service) StartIfLeader() {
	go func() {
		time.Sleep(600 * time.Millisecond)
		s.mu.Lock()
		isL := s.isLeader(s.view)
		s.mu.Unlock()
		if isL {
			s.proposeCurrentHeight()
		}
	}()
}

// 节点消息入口：解码共识消息并进入流程处理。
func (s *Service) HandleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var msg common.ConsensusMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.process(msg)
	w.WriteHeader(http.StatusOK)
}

// 统一入口：按算法分发到 SBFT 或 HotStuff 系列处理逻辑。
func (s *Service) process(msg common.ConsensusMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg.Height != s.height || msg.View != s.view {
		return
	}
	hs := s.getHeightState(msg.Height)
	dk := common.DedupKey(msg)
	if _, ok := hs.Dedup[dk]; ok {
		return
	}
	hs.Dedup[dk] = struct{}{}

	switch s.alg {
	case "sbft":
		s.processSBFT(msg, hs)
	case "hotstuff":
		s.processHotStuff(msg, hs)
	case "fast-hotstuff":
		s.processOneVote(msg, hs, "FHSProposal", "FHSVote", "FHSCommitQC")
	case "hpbft":
		s.processOneVote(msg, hs, "HPProposal", "HPPrepareVote", "HPQC")
	}
}

// SBFT 简化流程：PrePrepare -> Prepare(回 leader) -> CommitProof(广播)。
func (s *Service) processSBFT(msg common.ConsensusMessage, hs *heightState) {
	switch msg.Type {
	case "PrePrepare":
		if common.Digest(msg.View, msg.Height, msg.Tx) != msg.Digest {
			return
		}
		hs.ProposalDigest = msg.Digest
		hs.ProposalTx = msg.Tx
		s.persistProposal(msg)
		if !s.validateAndExecuteLoad(msg.Tx) {
			return
		}
		m := crypto.VoteMessage("Prepare", msg.View, msg.Height, msg.Digest, s.selfID)
		sig := crypto.Sign(s.keys[s.selfID], m)
		s.persistVote(msg.View, msg.Digest)
		share := common.ConsensusMessage{Type: "Prepare", View: msg.View, Height: msg.Height, From: s.selfID, Digest: msg.Digest, SigShare: sig}
		s.sendTo(s.leaderID(msg.View), share)
	case "Prepare":
		if !s.isLeader(msg.View) {
			return
		}
		m := crypto.VoteMessage("Prepare", msg.View, msg.Height, msg.Digest, msg.From)
		if !crypto.Verify(s.keys[msg.From], m, msg.SigShare) {
			return
		}
		hs.Prepared[msg.From] = msg.SigShare
		s.persistPrepare(msg)
		if len(hs.Prepared) >= s.th.T && !hs.Done {
			shares := make([]string, 0, len(hs.Prepared))
			for _, sig := range hs.Prepared {
				shares = append(shares, sig)
			}
			proof := crypto.Aggregate(shares)
			if !crypto.VerifyAggregate(shares, proof) {
				return
			}
			commitProof := common.ConsensusMessage{Type: "CommitProof", View: msg.View, Height: msg.Height, From: s.selfID, Digest: msg.Digest, QC: proof}
			hs.Done = true
			s.persistQC(commitProof)
			s.persistCommittedBlock(commitProof.Digest)
			s.broadcast(commitProof)
			go s.reportEnd(msg.Height)
			go s.advanceHeight()
		}
	case "CommitProof":
		if hs.Done || msg.QC == "" {
			return
		}
		hs.Done = true
		s.persistQC(msg)
		s.persistCommittedBlock(msg.Digest)
		go s.reportEnd(msg.Height)
		go s.advanceHeight()
	}
}

// HotStuff 链式流程：proposal 携带 parent/highQC，三链形成后提交祖先块。
func (s *Service) processHotStuff(msg common.ConsensusMessage, hs *heightState) {
	switch msg.Type {
	case "HSProposal":
		block := common.Block{
			BlockID:        s.messageBlockID(msg),
			ParentBlockID:  msg.ParentID,
			JustifyBlockID: msg.JustifyID,
			JustifyView:    msg.JustifyView,
			Digest:         msg.Digest,
			View:           msg.View,
			Height:         msg.Height,
			Proposer:       msg.From,
			Tx:             append([]string(nil), msg.Tx...),
		}
		if !s.validateHotStuffProposal(block, msg) {
			return
		}
		s.registerHotStuffBlock(block)
		s.persistProposal(msg)
		s.updateLockedQCFromProposal(block, msg)
		if !s.validateAndExecuteLoad(msg.Tx) {
			return
		}
		if votedBlock, ok := s.hotstuffVoted[msg.View]; ok && votedBlock != block.BlockID {
			return
		}
		m := crypto.VoteMessage("HSVote", msg.View, msg.Height, block.BlockID, s.selfID)
		sig := crypto.Sign(s.keys[s.selfID], m)
		s.hotstuffVoted[msg.View] = block.BlockID
		s.persistVote(msg.View, block.BlockID)
		vote := common.ConsensusMessage{
			Type:     "HSVote",
			View:     msg.View,
			Height:   msg.Height,
			From:     s.selfID,
			BlockID:  block.BlockID,
			Digest:   block.BlockID,
			SigShare: sig,
		}
		s.sendTo(s.leaderID(msg.View), vote)
	case "HSVote":
		if !s.isLeader(msg.View) {
			return
		}
		blockID := s.messageBlockID(msg)
		m := crypto.VoteMessage("HSVote", msg.View, msg.Height, blockID, msg.From)
		if !crypto.Verify(s.keys[msg.From], m, msg.SigShare) {
			return
		}
		hs.Voted[msg.From] = msg.SigShare
		if len(hs.Voted) >= s.th.T && !hs.Done {
			shares := make([]string, 0, len(hs.Voted))
			for _, sig := range hs.Voted {
				shares = append(shares, sig)
			}
			qc := crypto.Aggregate(shares)
			qcMsg := common.ConsensusMessage{
				Type:    "HSQC",
				View:    msg.View,
				Height:  msg.Height,
				From:    s.selfID,
				BlockID: blockID,
				Digest:  blockID,
				QC:      qc,
			}
			hs.Done = true
			s.persistQC(qcMsg)
			s.updateHotStuffHighQC(qcMsg)
			s.persistHighQC(qcMsg)
			s.commitHotStuffAncestor(blockID)
			s.broadcast(qcMsg)
			go s.advanceHeight()
		}
	case "HSQC":
		if hs.Done {
			return
		}
		if s.messageBlockID(msg) == "" || msg.QC == "" {
			return
		}
		hs.Done = true
		s.persistQC(msg)
		s.updateHotStuffHighQC(msg)
		s.persistHighQC(msg)
		s.commitHotStuffAncestor(s.messageBlockID(msg))
		go s.advanceHeight()
	}
}

// Fast-HotStuff/HPBFT 的单轮提案-投票-形成 QC 的简化闭环。
func (s *Service) processOneVote(msg common.ConsensusMessage, hs *heightState, proposalType, voteType, qcType string) {
	switch msg.Type {
	case proposalType:
		if common.Digest(msg.View, msg.Height, msg.Tx) != msg.Digest {
			return
		}
		hs.ProposalDigest = msg.Digest
		hs.ProposalTx = msg.Tx
		s.persistProposal(msg)
		if !s.validateAndExecuteLoad(msg.Tx) {
			return
		}
		m := crypto.VoteMessage(voteType, msg.View, msg.Height, msg.Digest, s.selfID)
		sig := crypto.Sign(s.keys[s.selfID], m)
		s.persistVote(msg.View, msg.Digest)
		vote := common.ConsensusMessage{Type: voteType, View: msg.View, Height: msg.Height, From: s.selfID, Digest: msg.Digest, SigShare: sig}
		s.sendTo(s.leaderID(msg.View), vote)
	case voteType:
		if !s.isLeader(msg.View) {
			return
		}
		m := crypto.VoteMessage(voteType, msg.View, msg.Height, msg.Digest, msg.From)
		if !crypto.Verify(s.keys[msg.From], m, msg.SigShare) {
			return
		}
		hs.Voted[msg.From] = msg.SigShare
		if len(hs.Voted) >= s.th.T && !hs.Done {
			shares := make([]string, 0, len(hs.Voted))
			for _, sig := range hs.Voted {
				shares = append(shares, sig)
			}
			qc := crypto.Aggregate(shares)
			qcMsg := common.ConsensusMessage{Type: qcType, View: msg.View, Height: msg.Height, From: s.selfID, Digest: msg.Digest, QC: qc}
			hs.Done = true
			s.persistQC(qcMsg)
			s.persistHighQC(qcMsg)
			s.persistCommittedBlock(qcMsg.Digest)
			s.broadcast(qcMsg)
			go s.reportEnd(msg.Height)
			go s.advanceHeight()
		}
	case qcType:
		if hs.Done {
			return
		}
		hs.Done = true
		s.persistQC(msg)
		s.persistHighQC(msg)
		s.persistCommittedBlock(msg.Digest)
		go s.reportEnd(msg.Height)
		go s.advanceHeight()
	}
}

// 计算给定 view 的 leader ID。
func (s *Service) leaderID(view int) int {
	return ((view - 1) % s.cfg.N) + 1
}

// 生成当前高度的提案并广播。
func (s *Service) proposeCurrentHeight() {
	s.mu.Lock()
	height := s.height
	view := s.view
	highQC := s.hotstuffHighQC
	s.mu.Unlock()
	tx := generateTx(height)
	digest := common.Digest(view, height, tx)
	s.callStart(height, view, len(tx))
	var msg common.ConsensusMessage
	switch s.alg {
	case "sbft":
		msg = common.ConsensusMessage{Type: "PrePrepare", View: view, Height: height, From: s.selfID, Digest: digest, Tx: tx}
	case "hotstuff":
		msg = common.ConsensusMessage{
			Type:        "HSProposal",
			View:        view,
			Height:      height,
			From:        s.selfID,
			BlockID:     digest,
			ParentID:    highQC.BlockID,
			JustifyID:   highQC.BlockID,
			JustifyQC:   highQC.QC,
			JustifyView: highQC.View,
			Digest:      digest,
			Tx:          tx,
		}
		s.registerHotStuffBlock(common.Block{
			BlockID:        digest,
			ParentBlockID:  highQC.BlockID,
			JustifyBlockID: highQC.BlockID,
			JustifyView:    highQC.View,
			Digest:         digest,
			View:           view,
			Height:         height,
			Proposer:       s.selfID,
			Tx:             append([]string(nil), tx...),
		})
		s.persistProposal(msg)
	case "fast-hotstuff":
		msg = common.ConsensusMessage{Type: "FHSProposal", View: view, Height: height, From: s.selfID, Digest: digest, Tx: tx}
	case "hpbft":
		msg = common.ConsensusMessage{Type: "HPProposal", View: view, Height: height, From: s.selfID, Digest: digest, Tx: tx}
	}
	s.broadcast(msg)
}

// 向 client 上报 /start（记录延迟起点）。
func (s *Service) callStart(height, view, batch int) {
	body, _ := json.Marshal(common.StartRequest{Height: height, View: view, Start: time.Now().UnixNano(), Batch: batch})
	_, _ = http.Post(s.clientURL+"/start", "application/json", bytes.NewReader(body))
}

// 向 client 上报 /end（记录延迟终点）。
func (s *Service) reportEnd(height int) {
	body, _ := json.Marshal(common.EndRequest{Height: height, From: s.selfID, End: time.Now().UnixNano(), View: s.view})
	_, _ = http.Post(s.clientURL+"/end", "application/json", bytes.NewReader(body))
}

// 推进高度与 view，并在成为 leader 时触发下一轮提案。
func (s *Service) advanceHeight() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.height < 1 {
		return
	}
	s.height++
	s.view = s.height
	s.persistPosition()
	if s.isLeader(s.view) {
		go func(alg string) {
			if alg == "hotstuff" {
				time.Sleep(80 * time.Millisecond)
			}
			s.proposeCurrentHeight()
		}(s.alg)
	}
}

// 向所有节点广播共识消息。
func (s *Service) broadcast(msg common.ConsensusMessage) {
	for i := 1; i <= s.cfg.N; i++ {
		s.sendTo(i, msg)
	}
}

// 发送消息到指定节点，按算法路由到对应 HTTP 路径。
func (s *Service) sendTo(id int, msg common.ConsensusMessage) {
	addr := s.peerAddrs[id]
	var prefix string
	switch s.alg {
	case "sbft":
		prefix = "/sbft/message"
	case "hotstuff":
		prefix = "/hotstuff/message"
	case "fast-hotstuff":
		prefix = "/fast-hotstuff/message"
	case "hpbft":
		prefix = "/hpbft/message"
	}
	b, _ := json.Marshal(msg)
	go func() {
		resp, err := http.Post("http://"+addr+prefix, "application/json", bytes.NewReader(b))
		if err != nil {
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
}

// 获取或初始化指定高度的状态缓存。
func (s *Service) getHeightState(height int) *heightState {
	hs, ok := s.state[height]
	if !ok {
		hs = &heightState{Prepared: map[int]string{}, Committed: map[int]string{}, Voted: map[int]string{}, Dedup: map[string]struct{}{}}
		s.state[height] = hs
	}
	return hs
}

func (s *Service) initHotStuffState() {
	genesis := common.Block{
		BlockID:  "genesis",
		Digest:   "genesis",
		View:     0,
		Height:   0,
		Proposer: 0,
	}
	s.hotstuffBlocks[genesis.BlockID] = &hotstuffBlock{Block: genesis, Committed: true, Executed: true}
	s.hotstuffHighQC = common.QuorumCert{Type: "GenesisQC", BlockID: genesis.BlockID, View: 0, Height: 0, QC: "genesis-qc"}
	s.hotstuffLockedQC = s.hotstuffHighQC
}

func (s *Service) loadPersistedHotStuffState() {
	if s.stores == nil || s.alg != "hotstuff" {
		return
	}
	if qc, err := s.stores.State.LoadHighQC(); err == nil && qc.BlockID != "" {
		s.hotstuffHighQC = common.QuorumCert{Type: qc.QCType, BlockID: qc.BlockID, View: qc.View, Height: qc.Height, QC: qc.QC}
	} else if err != nil && !errors.Is(err, goleveldb.ErrNotFound) {
		log.Printf("node=%d load high qc: %v", s.selfID, err)
	}
	if qc, err := s.stores.State.LoadLockedQC(); err == nil && qc.BlockID != "" {
		s.hotstuffLockedQC = common.QuorumCert{Type: qc.QCType, BlockID: qc.BlockID, View: qc.View, Height: qc.Height, QC: qc.QC}
	} else if err != nil && !errors.Is(err, goleveldb.ErrNotFound) {
		log.Printf("node=%d load locked qc: %v", s.selfID, err)
	}
}

func (s *Service) messageBlockID(msg common.ConsensusMessage) string {
	if msg.BlockID != "" {
		return msg.BlockID
	}
	return msg.Digest
}

func (s *Service) registerHotStuffBlock(block common.Block) {
	if block.BlockID == "" {
		return
	}
	existing, ok := s.hotstuffBlocks[block.BlockID]
	if ok {
		if existing.Block.ParentBlockID == "" && block.ParentBlockID != "" {
			existing.Block.ParentBlockID = block.ParentBlockID
		}
		if existing.Block.JustifyBlockID == "" && block.JustifyBlockID != "" {
			existing.Block.JustifyBlockID = block.JustifyBlockID
		}
		if existing.Block.JustifyView == 0 && block.JustifyView != 0 {
			existing.Block.JustifyView = block.JustifyView
		}
		if len(existing.Block.Tx) == 0 && len(block.Tx) > 0 {
			existing.Block.Tx = append([]string(nil), block.Tx...)
		}
		return
	}
	s.hotstuffBlocks[block.BlockID] = &hotstuffBlock{Block: block}
}

func (s *Service) validateHotStuffProposal(block common.Block, msg common.ConsensusMessage) bool {
	if common.Digest(msg.View, msg.Height, msg.Tx) != msg.Digest {
		return false
	}
	if block.BlockID == "" || block.ParentBlockID == "" {
		return false
	}
	if _, ok := s.hotstuffBlocks[block.ParentBlockID]; !ok {
		return false
	}
	if block.JustifyBlockID != "" && block.JustifyBlockID != block.ParentBlockID {
		return false
	}
	if s.hotstuffLockedQC.BlockID != "" && s.hotstuffLockedQC.BlockID != "genesis" {
		if block.JustifyView < s.hotstuffLockedQC.View && !s.extendsHotStuff(block.ParentBlockID, s.hotstuffLockedQC.BlockID) {
			return false
		}
	}
	return true
}

func (s *Service) extendsHotStuff(blockID, ancestorID string) bool {
	if ancestorID == "" {
		return true
	}
	cur := blockID
	for cur != "" {
		if cur == ancestorID {
			return true
		}
		block, ok := s.hotstuffBlocks[cur]
		if !ok {
			return false
		}
		cur = block.Block.ParentBlockID
	}
	return false
}

func (s *Service) updateLockedQCFromProposal(block common.Block, msg common.ConsensusMessage) {
	if msg.JustifyQC == "" || msg.JustifyView < s.hotstuffLockedQC.View {
		return
	}
	qc := common.QuorumCert{
		Type:    "HSQC",
		BlockID: block.ParentBlockID,
		View:    msg.JustifyView,
		Height:  block.Height - 1,
		QC:      msg.JustifyQC,
	}
	s.hotstuffLockedQC = qc
	s.persistLockedQC(qc)
}

func (s *Service) updateHotStuffHighQC(msg common.ConsensusMessage) {
	blockID := s.messageBlockID(msg)
	qc := common.QuorumCert{
		Type:    msg.Type,
		BlockID: blockID,
		View:    msg.View,
		Height:  msg.Height,
		QC:      msg.QC,
	}
	if qc.View <= s.hotstuffHighQC.View {
		return
	}
	if block, ok := s.hotstuffBlocks[blockID]; ok {
		block.QC = &qc
	}
	s.hotstuffHighQC = qc
}

func (s *Service) commitHotStuffAncestor(blockID string) {
	block, ok := s.hotstuffBlocks[blockID]
	if !ok {
		return
	}
	parent, ok := s.hotstuffBlocks[block.Block.ParentBlockID]
	if !ok {
		return
	}
	grandParent, ok := s.hotstuffBlocks[parent.Block.ParentBlockID]
	if !ok || grandParent.Block.BlockID == "genesis" {
		return
	}
	if grandParent.Committed {
		return
	}
	grandParent.Committed = true
	if !grandParent.Executed {
		_ = s.validateAndExecuteLoad(grandParent.Block.Tx)
		grandParent.Executed = true
	}
	s.persistCommittedBlock(grandParent.Block.BlockID)
	go s.reportEnd(grandParent.Block.Height)
}

func (s *Service) loadPersistedPosition() {
	if s.stores == nil {
		return
	}
	if view, err := s.stores.State.LoadCurrentView(); err == nil && view > 0 {
		s.view = view
	} else if err != nil && !errors.Is(err, goleveldb.ErrNotFound) {
		log.Printf("node=%d load current view: %v", s.selfID, err)
	}
	if height, err := s.stores.State.LoadCurrentHeight(); err == nil && height > 0 {
		s.height = height
	} else if err != nil && !errors.Is(err, goleveldb.ErrNotFound) {
		log.Printf("node=%d load current height: %v", s.selfID, err)
	}
}

func (s *Service) persistPosition() {
	if s.stores == nil {
		return
	}
	if err := s.stores.State.SaveCurrentView(s.view); err != nil {
		log.Printf("node=%d save current view: %v", s.selfID, err)
	}
	if err := s.stores.State.SaveCurrentHeight(s.height); err != nil {
		log.Printf("node=%d save current height: %v", s.selfID, err)
	}
}

func (s *Service) persistProposal(msg common.ConsensusMessage) {
	if s.stores == nil {
		return
	}
	record := storage.BlockRecord{
		BlockID:       s.messageBlockID(msg),
		ParentBlockID: msg.ParentID,
		Alg:           s.alg,
		MessageType:   msg.Type,
		Digest:        msg.Digest,
		View:          msg.View,
		Height:        msg.Height,
		From:          msg.From,
		Tx:            append([]string(nil), msg.Tx...),
		CreatedAt:     time.Now().UnixNano(),
	}
	if err := s.stores.Blocks.SaveBlock(record); err != nil {
		log.Printf("node=%d save block %s: %v", s.selfID, record.BlockID, err)
	}
}

func (s *Service) persistVote(view int, blockID string) {
	if s.stores == nil {
		return
	}
	if err := s.stores.State.SaveVote(view, blockID); err != nil {
		log.Printf("node=%d save vote view=%d block=%s: %v", s.selfID, view, blockID, err)
	}
}

func (s *Service) persistPrepare(msg common.ConsensusMessage) {
	if s.stores == nil {
		return
	}
	record := storage.PrepareRecord{
		Alg:       s.alg,
		Digest:    msg.Digest,
		View:      msg.View,
		Height:    msg.Height,
		From:      msg.From,
		SigShare:  msg.SigShare,
		CreatedAt: time.Now().UnixNano(),
	}
	if err := s.stores.State.SavePrepare(record); err != nil {
		log.Printf("node=%d save prepare height=%d view=%d from=%d: %v", s.selfID, msg.Height, msg.View, msg.From, err)
	}
}

func (s *Service) persistQC(msg common.ConsensusMessage) {
	if s.stores == nil {
		return
	}
	record := storage.QCRecord{
		BlockID:   s.messageBlockID(msg),
		Alg:       s.alg,
		QCType:    msg.Type,
		Digest:    msg.Digest,
		View:      msg.View,
		Height:    msg.Height,
		From:      msg.From,
		QC:        msg.QC,
		CreatedAt: time.Now().UnixNano(),
	}
	if err := s.stores.Blocks.SaveQC(record); err != nil {
		log.Printf("node=%d save qc %s: %v", s.selfID, msg.Digest, err)
	}
	if msg.Type == "CommitProof" {
		if err := s.stores.State.SaveCommitProof(record); err != nil {
			log.Printf("node=%d save commit proof %s: %v", s.selfID, msg.Digest, err)
		}
	}
}

func (s *Service) persistHighQC(msg common.ConsensusMessage) {
	if s.stores == nil {
		return
	}
	record := storage.QCRecord{
		BlockID:   s.messageBlockID(msg),
		Alg:       s.alg,
		QCType:    msg.Type,
		Digest:    msg.Digest,
		View:      msg.View,
		Height:    msg.Height,
		From:      msg.From,
		QC:        msg.QC,
		CreatedAt: time.Now().UnixNano(),
	}
	if err := s.stores.State.SaveHighQC(record); err != nil {
		log.Printf("node=%d save high qc %s: %v", s.selfID, msg.Digest, err)
	}
}

func (s *Service) persistLockedQC(qc common.QuorumCert) {
	if s.stores == nil {
		return
	}
	record := storage.QCRecord{
		BlockID:   qc.BlockID,
		Alg:       s.alg,
		QCType:    qc.Type,
		Digest:    qc.BlockID,
		View:      qc.View,
		Height:    qc.Height,
		From:      s.selfID,
		QC:        qc.QC,
		CreatedAt: time.Now().UnixNano(),
	}
	if err := s.stores.State.SaveLockedQC(record); err != nil {
		log.Printf("node=%d save locked qc %s: %v", s.selfID, qc.BlockID, err)
	}
}

func (s *Service) persistCommittedBlock(blockID string) {
	if s.stores == nil {
		return
	}
	if err := s.stores.State.SaveLastCommittedBlock(blockID); err != nil {
		log.Printf("node=%d save committed block %s: %v", s.selfID, blockID, err)
	}
}

// validateAndExecuteLoad 对批量交易做字段、nonce、余额检查，并执行状态变更模拟。
func (s *Service) validateAndExecuteLoad(tx []string) bool {
	const accountCount = 1000
	const initialBalance = 100000

	balances := make([]int, accountCount+1)
	nextNonce := make([]int, accountCount+1)
	for i := 1; i <= accountCount; i++ {
		balances[i] = initialBalance
	}
	seen := make(map[string]struct{}, len(tx))
	totalFees := 0

	for _, line := range tx {
		stx, ok := parseSimulatedTx(line)
		if !ok {
			return false
		}
		if stx.From < 1 || stx.From > accountCount || stx.To < 1 || stx.To > accountCount || stx.From == stx.To {
			return false
		}
		if stx.Amount <= 0 || stx.Fee < 0 || stx.Nonce <= 0 {
			return false
		}
		dedupKey := fmt.Sprintf("%d:%d", stx.From, stx.Nonce)
		if _, exists := seen[dedupKey]; exists {
			return false
		}
		seen[dedupKey] = struct{}{}
		if stx.Nonce != nextNonce[stx.From]+1 {
			return false
		}
		cost := stx.Amount + stx.Fee
		if balances[stx.From] < cost {
			return false
		}

		balances[stx.From] -= cost
		balances[stx.To] += stx.Amount
		nextNonce[stx.From] = stx.Nonce
		totalFees += stx.Fee
	}

	_ = totalFees
	return true
}

func parseSimulatedTx(line string) (simulatedTx, bool) {
	parts := strings.Fields(line)
	if len(parts) != 5 {
		return simulatedTx{}, false
	}
	from, err := strconv.Atoi(parts[0])
	if err != nil {
		return simulatedTx{}, false
	}
	to, err := strconv.Atoi(parts[1])
	if err != nil {
		return simulatedTx{}, false
	}
	amount, err := strconv.Atoi(parts[2])
	if err != nil {
		return simulatedTx{}, false
	}
	nonce, err := strconv.Atoi(parts[3])
	if err != nil {
		return simulatedTx{}, false
	}
	fee, err := strconv.Atoi(parts[4])
	if err != nil {
		return simulatedTx{}, false
	}
	return simulatedTx{From: from, To: to, Amount: amount, Nonce: nonce, Fee: fee}, true
}

// 生成模拟交易负载：每个高度 100*height 条，包含 amount/nonce/fee 以支持更复杂校验。
func generateTx(height int) []string {
	const accountCount = 1000
	const initialBalance = 100000

	sz := height * 100
	tx := make([]string, 0, sz)
	balances := make([]int, accountCount+1)
	nextNonce := make([]int, accountCount+1)
	for i := 1; i <= accountCount; i++ {
		balances[i] = initialBalance
	}
	for i := 0; i < sz; i++ {
		from := 1 + rand.Intn(accountCount)
		retries := 0
		for balances[from] < 2 && retries < accountCount {
			from = 1 + rand.Intn(accountCount)
			retries++
		}
		to := 1 + rand.Intn(accountCount)
		for to == from {
			to = 1 + rand.Intn(accountCount)
		}
		maxSpend := balances[from]
		if maxSpend <= 1 {
			maxSpend = 2
		}
		fee := rand.Intn(3) + 1
		maxAmount := maxSpend - fee
		if maxAmount < 1 {
			maxAmount = 1
			fee = 0
		}
		if maxAmount > 50 {
			maxAmount = 50
		}
		amount := rand.Intn(maxAmount) + 1
		nonce := nextNonce[from] + 1
		balances[from] -= amount + fee
		balances[to] += amount
		nextNonce[from] = nonce
		tx = append(tx, fmt.Sprintf("%d %d %d %d %d", from, to, amount, nonce, fee))
	}
	return tx
}

// 构建节点 HTTP 路由，只启用当前算法对应的消息入口。
func BuildMux(enabled string, handler http.HandlerFunc) *http.ServeMux {
	mux := http.NewServeMux()
	prefixes := []string{"sbft", "hotstuff", "fast-hotstuff", "hpbft"}
	for _, p := range prefixes {
		path := "/" + p + "/message"
		if p == enabled {
			mux.HandleFunc(path, handler)
		} else {
			mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
		}
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	return mux
}

// 启动节点 HTTP 服务并进入共识流程。
func Run(selfID int, alg string) error {
	rdb := redisx.NewClient()
	s, err := New(rdb, selfID, alg)
	if err != nil {
		return err
	}
	defer s.stores.Close()
	mux := BuildMux(alg, s.HandleMessage)
	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.BasePort+selfID)
	log.Printf("node=%d alg=%s listen=%s N=%d t=%d q=%d", selfID, alg, addr, s.th.N, s.th.T, s.th.Q)
	s.StartIfLeader()
	return http.ListenAndServe(addr, mux)
}
