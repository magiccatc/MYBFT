package leveldbstore

import (
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"

	"mybft/internal/storage"
)

type BlockStore struct {
	db *leveldb.DB
}

func NewBlockStore(db *leveldb.DB) *BlockStore {
	return &BlockStore{db: db}
}

func (s *BlockStore) SaveBlock(block storage.BlockRecord) error {
	return putJSON(s.db, fmt.Sprintf("block:%s", block.BlockID), block)
}

func (s *BlockStore) GetBlock(id string) (storage.BlockRecord, error) {
	var block storage.BlockRecord
	err := getJSON(s.db, fmt.Sprintf("block:%s", id), &block)
	return block, err
}

func (s *BlockStore) SaveQC(qc storage.QCRecord) error {
	return putJSON(s.db, fmt.Sprintf("qc:%s", qc.BlockID), qc)
}

func (s *BlockStore) GetQC(blockID string) (storage.QCRecord, error) {
	var qc storage.QCRecord
	err := getJSON(s.db, fmt.Sprintf("qc:%s", blockID), &qc)
	return qc, err
}
