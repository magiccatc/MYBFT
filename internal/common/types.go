package common

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Thresholds struct {
	N int
	T int
	Q int
}

// 计算 BFT 门限：t=⌊2N/3⌋+1，q=⌊N/3⌋+1。
func CalcThresholds(n int) Thresholds {
	return Thresholds{N: n, T: (2*n)/3 + 1, Q: n/3 + 1}
}

type StartRequest struct {
	Height int   `json:"height"`
	Start  int64 `json:"start"`
	View   int   `json:"view,omitempty"`
	Batch  int   `json:"batch,omitempty"`
}

type EndRequest struct {
	Height int   `json:"height"`
	End    int64 `json:"end,omitempty"`
	From   int   `json:"from"`
	View   int   `json:"view,omitempty"`
}

type QuorumCert struct {
	Type    string `json:"type"`
	BlockID string `json:"block_id"`
	View    int    `json:"view"`
	Height  int    `json:"height"`
	QC      string `json:"qc"`
}

type Block struct {
	BlockID        string   `json:"block_id"`
	ParentBlockID  string   `json:"parent_block_id,omitempty"`
	JustifyBlockID string   `json:"justify_block_id,omitempty"`
	JustifyView    int      `json:"justify_view,omitempty"`
	Digest         string   `json:"digest"`
	View           int      `json:"view"`
	Height         int      `json:"height"`
	Proposer       int      `json:"proposer"`
	Tx             []string `json:"tx,omitempty"`
}

type ConsensusMessage struct {
	Type        string   `json:"type"`
	View        int      `json:"view"`
	Height      int      `json:"height"`
	From        int      `json:"from"`
	BlockID     string   `json:"block_id,omitempty"`
	ParentID    string   `json:"parent_id,omitempty"`
	JustifyID   string   `json:"justify_id,omitempty"`
	JustifyQC   string   `json:"justify_qc,omitempty"`
	JustifyView int      `json:"justify_view,omitempty"`
	Digest      string   `json:"digest"`
	Tx          []string `json:"tx,omitempty"`
	SigShare    string   `json:"sig_share,omitempty"`
	SigFull     string   `json:"sig_full,omitempty"`
	QC          string   `json:"qc,omitempty"`
	SigAgg      string   `json:"sig_agg,omitempty"`
	SigAggFull  string   `json:"sig_agg_full,omitempty"`
}

// 生成共识消息摘要：基于 view/height 与交易内容的双重哈希。
func Digest(view, height int, tx []string) string {
	txRaw := strings.Join(tx, "\n")
	txDigest := sha256.Sum256([]byte(txRaw))
	raw := fmt.Sprintf("view=%d|height=%d|tx=%s", view, height, hex.EncodeToString(txDigest[:]))
	d := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(d[:])
}

// 生成稳定顺序的 JSON（用于签名/验签的一致性输入）。
func CanonicalJSON(v map[string]any) []byte {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		vv, _ := json.Marshal(v[k])
		parts = append(parts, fmt.Sprintf("\"%s\":%s", k, string(vv)))
	}
	return []byte("{" + strings.Join(parts, ",") + "}")
}

// 生成消息去重键，避免同一 view/height/digest 重复处理。
func DedupKey(msg ConsensusMessage) string {
	return fmt.Sprintf("%d:%d:%s:%d:%s", msg.View, msg.Height, msg.Digest, msg.From, msg.Type)
}
