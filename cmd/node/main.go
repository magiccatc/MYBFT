package main

import (
	"log"
	"os"
	"strconv"

	"mybft/internal/nodesvc"
)

// 启动单个共识节点，按指定算法处理消息并参与闭环流程。
func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: node id alg")
	}
	id, err := strconv.Atoi(os.Args[1])
	if err != nil || id < 1 {
		log.Fatal("invalid node id")
	}
	alg := os.Args[2]
	switch alg {
	case "sbft", "hotstuff", "fast-hotstuff", "hpbft":
	default:
		log.Fatalf("invalid alg: %s", alg)
	}
	if err := nodesvc.Run(id, alg); err != nil {
		log.Fatal(err)
	}
}
