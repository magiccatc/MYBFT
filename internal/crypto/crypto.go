package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

// 使用 HMAC-SHA256 生成演示用签名份额。
func Sign(sk string, msg []byte) string {
	h := hmac.New(sha256.New, []byte(sk))
	h.Write(msg)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// 通过重算签名的方式验证份额。
func Verify(sk string, msg []byte, sig string) bool {
	return Sign(sk, msg) == sig
}

// 将签名份额排序后聚合，模拟门限签名/QC 生成。
func Aggregate(shares []string) string {
	s := append([]string(nil), shares...)
	sort.Strings(s)
	raw := strings.Join(s, "|")
	h := sha256.Sum256([]byte(raw))
	return base64.StdEncoding.EncodeToString(h[:])
}

// 通过重算聚合值验证 QC/聚合签名。
func VerifyAggregate(shares []string, full string) bool {
	return Aggregate(shares) == full
}

// 生成投票/提交等消息的规范化字节序列（用于签名）。
func VoteMessage(msgType string, view, height int, digest string, from int) []byte {
	return []byte(fmt.Sprintf("{\"digest\":\"%s\",\"from\":%d,\"height\":%d,\"type\":\"%s\",\"view\":%d}", digest, from, height, msgType, view))
}
