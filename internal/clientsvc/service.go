package clientsvc

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"mybft/internal/common"
	"mybft/internal/redisx"
	"mybft/internal/storage"
	leveldbstore "mybft/internal/storage/leveldb"
)

type Service struct {
	mu                sync.Mutex
	rdb               *redisx.Client
	n                 int
	q                 int
	windowSeconds     int
	throughputSamples []throughputSample
	stores            *leveldbstore.ClientStores
}

type throughputSample struct {
	recordedAt time.Time
	txCount    int
}

// 创建 client 逻辑服务，计算 q=floor(N/3)+1 作为结束阈值，并初始化吞吐量窗口。
func New(n int) (*Service, error) {
	windowSeconds := 5
	if raw := os.Getenv("MYBFT_TPS_WINDOW_SECONDS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			windowSeconds = v
		}
	}
	dataRoot := os.Getenv("MYBFT_DATA_DIR")
	if dataRoot == "" {
		dataRoot = "data"
	}
	stores, err := leveldbstore.OpenClientStores(dataRoot)
	if err != nil {
		return nil, err
	}
	s := &Service{rdb: redisx.NewClient(), n: n, q: n/3 + 1, windowSeconds: windowSeconds, stores: stores}
	s.loadRecentThroughputSamples()
	return s, nil
}

// 处理 /start：按 height 独立记录起始时间，支持流水线下的乱序 commit。
func (s *Service) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req common.StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	now := time.Now().UnixNano()
	h := strconv.Itoa(req.Height)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok, _ := s.rdb.HExists("latency:start", h); !ok {
		s.rdb.HSet("latency:start", map[string]string{h: strconv.FormatInt(now, 10)})
		s.rdb.HSet("latency:batch", map[string]string{h: strconv.Itoa(req.Batch)})
		s.rdb.HSet("latency:reply", map[string]string{h: "0"})
		s.rdb.Del("latency:dedup:" + h)
		s.rdb.HDel("latency:end", h)
		s.rdb.HDel("latency:printed", h)
		log.Printf("ts=%d role=client id=0 event=start_recorded height=%d reset=end,printed", now, req.Height)
	} else {
		log.Printf("ts=%d role=client id=0 event=duplicate_start_ignored height=%d", now, req.Height)
	}
	w.WriteHeader(http.StatusOK)
}

// 处理 /end：对发送方去重，累积到 q 后打印延迟。
func (s *Service) handleEnd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req common.EndRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	now := time.Now().UnixNano()
	h := strconv.Itoa(req.Height)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok, _ := s.rdb.HExists("latency:start", h); !ok {
		log.Printf("ts=%d role=client id=0 event=end_without_start_dropped height=%d from=%d", now, req.Height, req.From)
		w.WriteHeader(http.StatusOK)
		return
	}
	if printed, _ := s.rdb.HGet("latency:printed", h); printed == "1" {
		w.WriteHeader(http.StatusOK)
		return
	}
	added, _ := s.rdb.SAdd("latency:dedup:"+h, strconv.Itoa(req.From))
	if added == 0 {
		log.Printf("ts=%d role=client id=0 event=end_duplicate_ignored height=%d from=%d", now, req.Height, req.From)
		w.WriteHeader(http.StatusOK)
		return
	}
	replies, _ := s.rdb.HIncrBy("latency:reply", h, 1)
	log.Printf("ts=%d role=client id=0 event=end_accepted height=%d from=%d reply=%d", now, req.Height, req.From, replies)
	if int(replies) == s.q {
		s.rdb.HSet("latency:end", map[string]string{h: strconv.FormatInt(now, 10)})
		startRaw, _ := s.rdb.HGet("latency:start", h)
		start, _ := strconv.ParseInt(startRaw, 10, 64)
		latency := float64(now-start) / 1e9
		batch := txCountForHeight(s.rdb, h, req.Height)
		recordedAt := time.Unix(0, now)
		throughput := s.recordThroughputSample(batch, recordedAt)
		s.persistMetric(req.Height, latency, batch, throughput, recordedAt)
		fmt.Printf("height %d latency is %f batch is %d throughput is %f tx/s\n", req.Height, latency, batch, throughput)
		s.rdb.HSet("latency:printed", map[string]string{h: "1"})
	}
	w.WriteHeader(http.StatusOK)
}

func txCountForHeight(rdb *redisx.Client, heightKey string, height int) int {
	raw, err := rdb.HGet("latency:batch", heightKey)
	if err == nil {
		if batch, parseErr := strconv.Atoi(raw); parseErr == nil && batch >= 0 {
			return batch
		}
	}
	return 100 * height
}

func (s *Service) recordThroughputSample(txCount int, recordedAt time.Time) float64 {
	s.throughputSamples = append(s.throughputSamples, throughputSample{recordedAt: recordedAt, txCount: txCount})
	if s.stores != nil {
		if err := s.stores.Metrics.AppendThroughputSample(storage.ThroughputSampleRecord{
			RecordedAt: recordedAt.UnixNano(),
			TxCount:    txCount,
		}); err != nil {
			log.Printf("client save throughput sample: %v", err)
		}
	}
	windowStart := recordedAt.Add(-time.Duration(s.windowSeconds) * time.Second)
	pruned := s.throughputSamples[:0]
	totalTx := 0
	for _, sample := range s.throughputSamples {
		if sample.recordedAt.Before(windowStart) {
			continue
		}
		pruned = append(pruned, sample)
		totalTx += sample.txCount
	}
	s.throughputSamples = pruned
	return float64(totalTx) / float64(s.windowSeconds)
}

func (s *Service) loadRecentThroughputSamples() {
	if s.stores == nil {
		return
	}
	since := time.Now().Add(-time.Duration(s.windowSeconds) * time.Second).UnixNano()
	records, err := s.stores.Metrics.LoadThroughputSamplesSince(since)
	if err != nil {
		log.Printf("client load throughput samples: %v", err)
		return
	}
	for _, record := range records {
		s.throughputSamples = append(s.throughputSamples, throughputSample{
			recordedAt: time.Unix(0, record.RecordedAt),
			txCount:    record.TxCount,
		})
	}
}

func (s *Service) persistMetric(height int, latency float64, batch int, throughput float64, recordedAt time.Time) {
	if s.stores == nil {
		return
	}
	record := storage.MetricRecord{
		Height:        height,
		Latency:       latency,
		Batch:         batch,
		Throughput:    throughput,
		RecordedAt:    recordedAt.UnixNano(),
		WindowSeconds: s.windowSeconds,
	}
	if err := s.stores.Metrics.SaveMetric(record); err != nil {
		log.Printf("client save metric height=%d: %v", height, err)
	}
}

// 启动 HTTP 服务，暴露 /start 与 /end 供节点上报。
func Run(n int) error {
	rdb := redisx.NewClient()
	rdb.HSet("cluster:config", map[string]string{"N": strconv.Itoa(n), "basePort": "9000", "clientAddr": "127.0.0.1:8000"})
	s, err := New(n)
	if err != nil {
		return err
	}
	defer s.stores.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/end", s.handleEnd)
	log.Printf("client listen=127.0.0.1:8000 n=%d q=%d", n, n/3+1)
	return http.ListenAndServe("127.0.0.1:8000", mux)
}
