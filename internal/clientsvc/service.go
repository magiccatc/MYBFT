package clientsvc

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"mybft/internal/common"
	"mybft/internal/redisx"
)

type Service struct {
	mu            sync.Mutex
	rdb           *redisx.Client
	n             int
	q             int
	currentHeight int
}

func New(n int) *Service {
	return &Service{rdb: redisx.NewClient(), n: n, q: n/3 + 1}
}

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
	if req.Height > s.currentHeight {
		s.currentHeight = req.Height
		s.rdb.HSet("latency:start", map[string]string{h: strconv.FormatInt(now, 10)})
		s.rdb.HSet("latency:reply", map[string]string{h: "0"})
		s.rdb.Del("latency:dedup:" + h)
		s.rdb.HDel("latency:end", h)
		s.rdb.HDel("latency:printed", h)
		log.Printf("ts=%d role=client id=0 event=start_recorded height=%d reset=end,printed", now, req.Height)
	} else if req.Height == s.currentHeight {
		log.Printf("ts=%d role=client id=0 event=duplicate_start_ignored height=%d", now, req.Height)
	} else {
		log.Printf("ts=%d role=client id=0 event=stale_start_dropped height=%d", now, req.Height)
	}
	w.WriteHeader(http.StatusOK)
}

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
	if exists, _ := s.rdb.HExists("latency:end", h); !exists {
		s.rdb.HSet("latency:end", map[string]string{h: strconv.FormatInt(now, 10)})
	}
	log.Printf("ts=%d role=client id=0 event=end_accepted height=%d from=%d reply=%d", now, req.Height, req.From, replies)
	if int(replies) == s.q {
		startRaw, _ := s.rdb.HGet("latency:start", h)
		endRaw, _ := s.rdb.HGet("latency:end", h)
		start, _ := strconv.ParseInt(startRaw, 10, 64)
		end, _ := strconv.ParseInt(endRaw, 10, 64)
		latency := float64(end-start) / 1e9
		fmt.Printf("height %d latency is %f batch is %d\n", req.Height, latency, 200*req.Height)
		s.rdb.HSet("latency:printed", map[string]string{h: "1"})
	}
	w.WriteHeader(http.StatusOK)
}

func Run(n int) error {
	rdb := redisx.NewClient()
	rdb.HSet("cluster:config", map[string]string{"N": strconv.Itoa(n), "basePort": "9000", "clientAddr": "127.0.0.1:8000"})
	s := New(n)
	mux := http.NewServeMux()
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/end", s.handleEnd)
	log.Printf("client listen=127.0.0.1:8000 n=%d q=%d", n, n/3+1)
	return http.ListenAndServe("127.0.0.1:8000", mux)
}
