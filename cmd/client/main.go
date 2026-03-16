package main

import (
	"log"
	"os"
	"strconv"

	"mybft/internal/clientsvc"
)

// 启动 client 进程，接收 /start 与 /end 以统计各高度的延迟指标。
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
