package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strconv"

	"mybft/internal/common"
	"mybft/internal/redisx"
)

func randKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: genkey N")
	}
	n, err := strconv.Atoi(os.Args[1])
	if err != nil || n < 1 {
		log.Fatal("invalid N")
	}
	rdb := redisx.NewClient()
	th := common.CalcThresholds(n)
	rdb.HSet("cluster:config", map[string]string{"N": strconv.Itoa(n), "basePort": "9000", "clientAddr": "127.0.0.1:8000", "t": strconv.Itoa(th.T)})
	for i := 1; i <= n; i++ {
		key := fmt.Sprintf("Node:%d", i)
		rdb.HSet(key, map[string]string{
			"threshold_pk": "demo-threshold-pk",
			"threshold_sk": randKey(),
			"agg_pk":       "demo-agg-pk",
			"agg_sk":       randKey(),
		})
	}
	log.Printf("generated keys for N=%d t=%d q=%d", n, th.T, th.Q)
}
