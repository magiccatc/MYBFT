package leveldbstore

import (
	"fmt"
	"strconv"

	"github.com/syndtr/goleveldb/leveldb"

	"mybft/internal/storage"
)

type StateStore struct {
	db *leveldb.DB
}

func NewStateStore(db *leveldb.DB) *StateStore {
	return &StateStore{db: db}
}

func (s *StateStore) SaveCurrentView(view int) error {
	return s.db.Put([]byte("meta:currentView"), []byte(strconv.Itoa(view)), nil)
}

func (s *StateStore) LoadCurrentView() (int, error) {
	return s.loadInt("meta:currentView")
}

func (s *StateStore) SaveCurrentHeight(height int) error {
	return s.db.Put([]byte("meta:currentHeight"), []byte(strconv.Itoa(height)), nil)
}

func (s *StateStore) LoadCurrentHeight() (int, error) {
	return s.loadInt("meta:currentHeight")
}

func (s *StateStore) SaveHighQC(qc storage.QCRecord) error {
	return putJSON(s.db, "meta:highQC", qc)
}

func (s *StateStore) LoadHighQC() (storage.QCRecord, error) {
	var qc storage.QCRecord
	err := getJSON(s.db, "meta:highQC", &qc)
	return qc, err
}

func (s *StateStore) SaveLockedQC(qc storage.QCRecord) error {
	return putJSON(s.db, "meta:lockedQC", qc)
}

func (s *StateStore) LoadLockedQC() (storage.QCRecord, error) {
	var qc storage.QCRecord
	err := getJSON(s.db, "meta:lockedQC", &qc)
	return qc, err
}

func (s *StateStore) SaveLastCommittedBlock(blockID string) error {
	return s.db.Put([]byte("meta:lastCommittedBlock"), []byte(blockID), nil)
}

func (s *StateStore) LoadLastCommittedBlock() (string, error) {
	raw, err := s.db.Get([]byte("meta:lastCommittedBlock"), nil)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *StateStore) SaveVote(view int, blockID string) error {
	return s.db.Put([]byte(fmt.Sprintf("vote:%d", view)), []byte(blockID), nil)
}

func (s *StateStore) LoadVote(view int) (string, error) {
	raw, err := s.db.Get([]byte(fmt.Sprintf("vote:%d", view)), nil)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *StateStore) SavePrepare(record storage.PrepareRecord) error {
	key := fmt.Sprintf("prepare:%d:%d:%d", record.Height, record.View, record.From)
	return putJSON(s.db, key, record)
}

func (s *StateStore) SaveCommitProof(qc storage.QCRecord) error {
	key := fmt.Sprintf("commitproof:%d:%d", qc.Height, qc.View)
	return putJSON(s.db, key, qc)
}

func (s *StateStore) loadInt(key string) (int, error) {
	raw, err := s.db.Get([]byte(key), nil)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(raw))
}
