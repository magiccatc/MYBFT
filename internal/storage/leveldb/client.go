package leveldbstore

import (
	"os"
	"path/filepath"

	"github.com/syndtr/goleveldb/leveldb"

	"mybft/internal/storage"
)

type ClientStores struct {
	Metrics storage.MetricsStore

	metricsDB *leveldb.DB
}

func OpenClientStores(root string) (*ClientStores, error) {
	metricsPath := filepath.Join(root, "client", "metrics", "leveldb")
	if err := os.MkdirAll(metricsPath, 0o755); err != nil {
		return nil, err
	}
	metricsDB, err := leveldb.OpenFile(metricsPath, nil)
	if err != nil {
		return nil, err
	}
	return &ClientStores{
		Metrics:   NewMetricsStore(metricsDB),
		metricsDB: metricsDB,
	}, nil
}

func (s *ClientStores) Close() error {
	if s == nil {
		return nil
	}
	if s.metricsDB != nil {
		_ = s.metricsDB.Close()
	}
	return nil
}
