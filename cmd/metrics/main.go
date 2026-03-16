package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"mybft/internal/storage"
	leveldbstore "mybft/internal/storage/leveldb"
)

func main() {
	dataRoot := os.Getenv("MYBFT_DATA_DIR")
	if dataRoot == "" {
		dataRoot = "data"
	}
	stores, err := leveldbstore.OpenClientStores(dataRoot)
	if err != nil {
		log.Fatal(err)
	}
	defer stores.Close()

	switch len(os.Args) {
	case 1:
		records, err := stores.Metrics.ListMetrics()
		if err != nil {
			log.Fatal(err)
		}
		if len(records) == 0 {
			fmt.Println("no metrics found")
			return
		}
		for _, record := range records {
			printMetric(record)
		}
	case 2:
		height, err := strconv.Atoi(os.Args[1])
		if err != nil || height < 1 {
			log.Fatal("usage: metrics [height]")
		}
		record, err := stores.Metrics.LoadMetric(height)
		if err != nil {
			log.Fatal(err)
		}
		printMetric(record)
	default:
		log.Fatal("usage: metrics [height]")
	}
}

func printMetric(record storage.MetricRecord) {
	recordedAt := time.Unix(0, record.RecordedAt).Format(time.RFC3339)
	fmt.Printf(
		"height=%d latency=%f batch=%d throughput=%f tx/s window=%ds recorded_at=%s\n",
		record.Height,
		record.Latency,
		record.Batch,
		record.Throughput,
		record.WindowSeconds,
		recordedAt,
	)
}
