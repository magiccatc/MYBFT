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

func CalcThresholds(n int) Thresholds {
	return Thresholds{N: n, T: (2*n)/3 + 1, Q: n/3 + 1}
}

type StartRequest struct {
	Height int   `json:"height"`
	Start  int64 `json:"start"`
	View   int   `json:"view,omitempty"`
}

type EndRequest struct {
	Height int   `json:"height"`
	End    int64 `json:"end,omitempty"`
	From   int   `json:"from"`
	View   int   `json:"view,omitempty"`
}

type ConsensusMessage struct {
	Type       string   `json:"type"`
	View       int      `json:"view"`
	Height     int      `json:"height"`
	From       int      `json:"from"`
	Digest     string   `json:"digest"`
	Tx         []string `json:"tx,omitempty"`
	SigShare   string   `json:"sig_share,omitempty"`
	SigFull    string   `json:"sig_full,omitempty"`
	QC         string   `json:"qc,omitempty"`
	SigAgg     string   `json:"sig_agg,omitempty"`
	SigAggFull string   `json:"sig_agg_full,omitempty"`
}

func Digest(view, height int, tx []string) string {
	txRaw := strings.Join(tx, "\n")
	txDigest := sha256.Sum256([]byte(txRaw))
	raw := fmt.Sprintf("view=%d|height=%d|tx=%s", view, height, hex.EncodeToString(txDigest[:]))
	d := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(d[:])
}

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

func DedupKey(msg ConsensusMessage) string {
	return fmt.Sprintf("%d:%d:%s:%d:%s", msg.View, msg.Height, msg.Digest, msg.From, msg.Type)
}
