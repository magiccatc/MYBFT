package storage

type BlockRecord struct {
	BlockID       string   `json:"block_id"`
	ParentBlockID string   `json:"parent_block_id,omitempty"`
	Alg           string   `json:"alg"`
	MessageType   string   `json:"message_type"`
	Digest        string   `json:"digest"`
	View          int      `json:"view"`
	Height        int      `json:"height"`
	From          int      `json:"from"`
	Tx            []string `json:"tx,omitempty"`
	CreatedAt     int64    `json:"created_at"`
}

type QCRecord struct {
	BlockID   string `json:"block_id"`
	Alg       string `json:"alg"`
	QCType    string `json:"qc_type"`
	Digest    string `json:"digest"`
	View      int    `json:"view"`
	Height    int    `json:"height"`
	From      int    `json:"from"`
	QC        string `json:"qc"`
	CreatedAt int64  `json:"created_at"`
}

type PrepareRecord struct {
	Alg       string `json:"alg"`
	Digest    string `json:"digest"`
	View      int    `json:"view"`
	Height    int    `json:"height"`
	From      int    `json:"from"`
	SigShare  string `json:"sig_share"`
	CreatedAt int64  `json:"created_at"`
}

type ThroughputSampleRecord struct {
	RecordedAt int64 `json:"recorded_at"`
	TxCount    int   `json:"tx_count"`
}

type MetricRecord struct {
	Height        int     `json:"height"`
	Latency       float64 `json:"latency"`
	Batch         int     `json:"batch"`
	Throughput    float64 `json:"throughput"`
	RecordedAt    int64   `json:"recorded_at"`
	WindowSeconds int     `json:"window_seconds"`
}

type BlockStore interface {
	SaveBlock(block BlockRecord) error
	GetBlock(id string) (BlockRecord, error)
	SaveQC(qc QCRecord) error
	GetQC(blockID string) (QCRecord, error)
}

type StateStore interface {
	SaveCurrentView(view int) error
	LoadCurrentView() (int, error)
	SaveCurrentHeight(height int) error
	LoadCurrentHeight() (int, error)
	SaveHighQC(qc QCRecord) error
	LoadHighQC() (QCRecord, error)
	SaveLockedQC(qc QCRecord) error
	LoadLockedQC() (QCRecord, error)
	SaveLastCommittedBlock(blockID string) error
	LoadLastCommittedBlock() (string, error)
	SaveVote(view int, blockID string) error
	LoadVote(view int) (string, error)
	SavePrepare(record PrepareRecord) error
	SaveCommitProof(qc QCRecord) error
}

type MetricsStore interface {
	SaveMetric(record MetricRecord) error
	LoadMetric(height int) (MetricRecord, error)
	ListMetrics() ([]MetricRecord, error)
	AppendThroughputSample(record ThroughputSampleRecord) error
	LoadThroughputSamplesSince(sinceUnixNano int64) ([]ThroughputSampleRecord, error)
}
