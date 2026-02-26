package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

func Sign(sk string, msg []byte) string {
	h := hmac.New(sha256.New, []byte(sk))
	h.Write(msg)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func Verify(sk string, msg []byte, sig string) bool {
	return Sign(sk, msg) == sig
}

func Aggregate(shares []string) string {
	s := append([]string(nil), shares...)
	sort.Strings(s)
	raw := strings.Join(s, "|")
	h := sha256.Sum256([]byte(raw))
	return base64.StdEncoding.EncodeToString(h[:])
}

func VerifyAggregate(shares []string, full string) bool {
	return Aggregate(shares) == full
}

func VoteMessage(msgType string, view, height int, digest string, from int) []byte {
	return []byte(fmt.Sprintf("{\"digest\":\"%s\",\"from\":%d,\"height\":%d,\"type\":\"%s\",\"view\":%d}", digest, from, height, msgType, view))
}
