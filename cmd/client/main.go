package main

import (
	"log"
	"os"
	"strconv"

	"mybft/internal/clientsvc"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: client N")
	}
	n, err := strconv.Atoi(os.Args[1])
	if err != nil || n < 1 {
		log.Fatal("invalid N")
	}
	if err := clientsvc.Run(n); err != nil {
		log.Fatal(err)
	}
}
