package nodesvc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"mybft/internal/common"
	"mybft/internal/crypto"
	"mybft/internal/redisx"
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

type Service struct {
	mu         sync.Mutex
	rdb        *redisx.Client
	selfID     int
	alg        string
	cfg        redisx.ClusterConfig
	th         common.Thresholds
	height     int
	view       int
	leaderMode bool
	keys       map[int]string
	peerAddrs  map[int]string
	clientURL  string
	state      map[int]*heightState
}

func New(rdb *redisx.Client, selfID int, alg string) (*Service, error) {
	cfg, err := redisx.ReadClusterConfig(rdb)
	if err != nil {
		return nil, err
	}
	s := &Service{rdb: rdb, selfID: selfID, alg: alg, cfg: cfg, th: common.CalcThresholds(cfg.N), height: 1, view: 1, keys: map[int]string{}, peerAddrs: map[int]string{}, state: map[int]*heightState{}, clientURL: "http://" + cfg.ClientAddr}
	for i := 1; i <= cfg.N; i++ {
		sk, err := rdb.HGet(fmt.Sprintf("Node:%d", i), "threshold_sk")
		if err != nil {
			return nil, fmt.Errorf("load key Node:%d: %w", i, err)
		}
		s.keys[i] = sk
		s.peerAddrs[i] = fmt.Sprintf("127.0.0.1:%d", cfg.BasePort+i)
	}
	s.leaderMode = s.isLeader(s.view)
	return s, nil
}

func (s *Service) isLeader(view int) bool {
	if s.alg == "pbft" {
		return s.selfID == 1
	}
	leader := ((view - 1) % s.cfg.N) + 1
	return s.selfID == leader
}

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
	case "pbft":
		s.processPBFT(msg, hs)
	case "hotstuff":
		s.processOneVote(msg, hs, "HSProposal", "HSVote", "HSQC")
	case "fast-hotstuff":
		s.processOneVote(msg, hs, "FHSProposal", "FHSVote", "FHSCommitQC")
	case "hpbft":
		s.processOneVote(msg, hs, "HPProposal", "HPPrepareVote", "HPQC")
	}
}

func (s *Service) processPBFT(msg common.ConsensusMessage, hs *heightState) {
	switch msg.Type {
	case "PrePrepare":
		if common.Digest(msg.View, msg.Height, msg.Tx) != msg.Digest {
			return
		}
		hs.ProposalDigest = msg.Digest
		hs.ProposalTx = msg.Tx
		s.executeLoad(msg.Tx)
		s.sendPrepare(msg.Digest)
	case "Prepare":
		m := crypto.VoteMessage("Prepare", msg.View, msg.Height, msg.Digest, msg.From)
		if !crypto.Verify(s.keys[msg.From], m, msg.SigShare) {
			return
		}
		hs.Prepared[msg.From] = msg.SigShare
		if len(hs.Prepared) >= s.th.T {
			s.sendCommit(msg.Digest)
		}
	case "Commit":
		m := crypto.VoteMessage("Commit", msg.View, msg.Height, msg.Digest, msg.From)
		if !crypto.Verify(s.keys[msg.From], m, msg.SigShare) {
			return
		}
		hs.Committed[msg.From] = msg.SigShare
		if len(hs.Committed) >= s.th.T && !hs.Done {
			shares := make([]string, 0, len(hs.Committed))
			for _, sig := range hs.Committed {
				shares = append(shares, sig)
			}
			full := crypto.Aggregate(shares)
			if !crypto.VerifyAggregate(shares, full) {
				return
			}
			hs.Done = true
			go s.reportEnd(msg.Height)
			go s.advanceHeight()
		}
	}
}

func (s *Service) processOneVote(msg common.ConsensusMessage, hs *heightState, proposalType, voteType, qcType string) {
	switch msg.Type {
	case proposalType:
		if common.Digest(msg.View, msg.Height, msg.Tx) != msg.Digest {
			return
		}
		hs.ProposalDigest = msg.Digest
		hs.ProposalTx = msg.Tx
		s.executeLoad(msg.Tx)
		m := crypto.VoteMessage(voteType, msg.View, msg.Height, msg.Digest, s.selfID)
		sig := crypto.Sign(s.keys[s.selfID], m)
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
			s.broadcast(qcMsg)
			go s.reportEnd(msg.Height)
			go s.advanceHeight()
		}
	case qcType:
		if hs.Done {
			return
		}
		hs.Done = true
		go s.reportEnd(msg.Height)
		go s.advanceHeight()
	}
}

func (s *Service) leaderID(view int) int {
	if s.alg == "pbft" {
		return 1
	}
	return ((view - 1) % s.cfg.N) + 1
}

func (s *Service) proposeCurrentHeight() {
	s.mu.Lock()
	height := s.height
	view := s.view
	s.mu.Unlock()
	tx := generateTx(height)
	digest := common.Digest(view, height, tx)
	s.callStart(height, view)
	var msg common.ConsensusMessage
	switch s.alg {
	case "pbft":
		msg = common.ConsensusMessage{Type: "PrePrepare", View: view, Height: height, From: s.selfID, Digest: digest, Tx: tx}
	case "hotstuff":
		msg = common.ConsensusMessage{Type: "HSProposal", View: view, Height: height, From: s.selfID, Digest: digest, Tx: tx}
	case "fast-hotstuff":
		msg = common.ConsensusMessage{Type: "FHSProposal", View: view, Height: height, From: s.selfID, Digest: digest, Tx: tx}
	case "hpbft":
		msg = common.ConsensusMessage{Type: "HPProposal", View: view, Height: height, From: s.selfID, Digest: digest, Tx: tx}
	}
	s.broadcast(msg)
}

func (s *Service) callStart(height, view int) {
	body, _ := json.Marshal(common.StartRequest{Height: height, View: view, Start: time.Now().UnixNano()})
	_, _ = http.Post(s.clientURL+"/start", "application/json", bytes.NewReader(body))
}

func (s *Service) reportEnd(height int) {
	body, _ := json.Marshal(common.EndRequest{Height: height, From: s.selfID, End: time.Now().UnixNano(), View: s.view})
	_, _ = http.Post(s.clientURL+"/end", "application/json", bytes.NewReader(body))
}

func (s *Service) advanceHeight() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.height < 1 {
		return
	}
	s.height++
	s.view = s.height
	if s.isLeader(s.view) {
		go s.proposeCurrentHeight()
	}
}

func (s *Service) sendPrepare(digest string) {
	m := crypto.VoteMessage("Prepare", s.view, s.height, digest, s.selfID)
	sig := crypto.Sign(s.keys[s.selfID], m)
	msg := common.ConsensusMessage{Type: "Prepare", View: s.view, Height: s.height, From: s.selfID, Digest: digest, SigShare: sig}
	s.broadcast(msg)
}

func (s *Service) sendCommit(digest string) {
	m := crypto.VoteMessage("Commit", s.view, s.height, digest, s.selfID)
	sig := crypto.Sign(s.keys[s.selfID], m)
	msg := common.ConsensusMessage{Type: "Commit", View: s.view, Height: s.height, From: s.selfID, Digest: digest, SigShare: sig}
	s.broadcast(msg)
}

func (s *Service) broadcast(msg common.ConsensusMessage) {
	for i := 1; i <= s.cfg.N; i++ {
		s.sendTo(i, msg)
	}
}

func (s *Service) sendTo(id int, msg common.ConsensusMessage) {
	addr := s.peerAddrs[id]
	var prefix string
	switch s.alg {
	case "pbft":
		prefix = "/pbft/message"
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

func (s *Service) getHeightState(height int) *heightState {
	hs, ok := s.state[height]
	if !ok {
		hs = &heightState{Prepared: map[int]string{}, Committed: map[int]string{}, Voted: map[int]string{}, Dedup: map[string]struct{}{}}
		s.state[height] = hs
	}
	return hs
}

func (s *Service) executeLoad(tx []string) {
	nums := make([]int, 1001)
	for i := range nums {
		nums[i] = i
	}
	for _, line := range tx {
		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}
		sid, _ := strconv.Atoi(parts[0])
		rid, _ := strconv.Atoi(parts[1])
		tn, _ := strconv.Atoi(parts[2])
		if sid >= 0 && sid < len(nums) && rid >= 0 && rid < len(nums) && sid != rid {
			nums[sid] += tn
			nums[rid] -= tn
		}
	}
}

func generateTx(height int) []string {
	sz := height * 100
	tx := make([]string, 0, sz)
	for i := 0; i < sz; i++ {
		a := rand.Intn(1001)
		b := rand.Intn(1001)
		for b == a {
			b = rand.Intn(1001)
		}
		n := rand.Intn(10) + 1
		tx = append(tx, fmt.Sprintf("%d %d %d", a, b, n))
	}
	return tx
}

func BuildMux(enabled string, handler http.HandlerFunc) *http.ServeMux {
	mux := http.NewServeMux()
	prefixes := []string{"pbft", "hotstuff", "fast-hotstuff", "hpbft"}
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

func Run(selfID int, alg string) error {
	rdb := redisx.NewClient()
	s, err := New(rdb, selfID, alg)
	if err != nil {
		return err
	}
	mux := BuildMux(alg, s.HandleMessage)
	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.BasePort+selfID)
	log.Printf("node=%d alg=%s listen=%s N=%d t=%d q=%d", selfID, alg, addr, s.th.N, s.th.T, s.th.Q)
	s.StartIfLeader()
	return http.ListenAndServe(addr, mux)
}
