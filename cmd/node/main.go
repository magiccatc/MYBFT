package main

import (
	"log"
	"os"
	"strconv"

	"mybft/internal/nodesvc"
)

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
	case "pbft", "hotstuff", "fast-hotstuff", "hpbft":
	default:
		log.Fatalf("invalid alg: %s", alg)
	}
	if err := nodesvc.Run(id, alg); err != nil {
		log.Fatal(err)
	}
}
