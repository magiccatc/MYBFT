package leveldbstore

import (
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"

	"mybft/internal/storage"
)

type MetricsStore struct {
	db *leveldb.DB
}

func NewMetricsStore(db *leveldb.DB) *MetricsStore {
	return &MetricsStore{db: db}
}

func (s *MetricsStore) SaveMetric(record storage.MetricRecord) error {
	return putJSON(s.db, fmt.Sprintf("metric:%09d", record.Height), record)
}

func (s *MetricsStore) LoadMetric(height int) (storage.MetricRecord, error) {
	var record storage.MetricRecord
	err := getJSON(s.db, fmt.Sprintf("metric:%09d", height), &record)
	return record, err
}

func (s *MetricsStore) ListMetrics() ([]storage.MetricRecord, error) {
	prefix := []byte("metric:")
	iter := s.db.NewIterator(util.BytesPrefix(prefix), nil)
	defer iter.Release()

	records := make([]storage.MetricRecord, 0)
	for iter.Next() {
		var record storage.MetricRecord
		if err := jsonUnmarshal(iter.Value(), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, iter.Error()
}

func (s *MetricsStore) AppendThroughputSample(record storage.ThroughputSampleRecord) error {
	return putJSON(s.db, fmt.Sprintf("sample:%020d", record.RecordedAt), record)
}

func (s *MetricsStore) LoadThroughputSamplesSince(sinceUnixNano int64) ([]storage.ThroughputSampleRecord, error) {
	prefix := []byte("sample:")
	iter := s.db.NewIterator(util.BytesPrefix(prefix), nil)
	defer iter.Release()

	records := make([]storage.ThroughputSampleRecord, 0)
	for iter.Next() {
		var record storage.ThroughputSampleRecord
		if err := jsonUnmarshal(iter.Value(), &record); err != nil {
			return nil, err
		}
		if record.RecordedAt < sinceUnixNano {
			continue
		}
		records = append(records, record)
	}
	return records, iter.Error()
}

func jsonUnmarshal(raw []byte, out any) error {
	return unmarshalJSON(raw, out)
}
