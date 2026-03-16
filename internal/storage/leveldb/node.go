package leveldbstore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/syndtr/goleveldb/leveldb"

	"mybft/internal/storage"
)

type NodeStores struct {
	Blocks storage.BlockStore
	State  storage.StateStore

	blocksDB *leveldb.DB
	stateDB  *leveldb.DB
}

func OpenNodeStores(root string, nodeID int) (*NodeStores, error) {
	base := filepath.Join(root, fmt.Sprintf("node-%d", nodeID))
	blocksPath := filepath.Join(base, "blocks", "leveldb")
	statePath := filepath.Join(base, "state", "leveldb")

	if err := os.MkdirAll(blocksPath, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(statePath, 0o755); err != nil {
		return nil, err
	}

	blocksDB, err := leveldb.OpenFile(blocksPath, nil)
	if err != nil {
		return nil, err
	}
	stateDB, err := leveldb.OpenFile(statePath, nil)
	if err != nil {
		_ = blocksDB.Close()
		return nil, err
	}

	return &NodeStores{
		Blocks:   NewBlockStore(blocksDB),
		State:    NewStateStore(stateDB),
		blocksDB: blocksDB,
		stateDB:  stateDB,
	}, nil
}

func (s *NodeStores) Close() error {
	if s == nil {
		return nil
	}
	if s.blocksDB != nil {
		_ = s.blocksDB.Close()
	}
	if s.stateDB != nil {
		_ = s.stateDB.Close()
	}
	return nil
}
