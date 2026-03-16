package leveldbstore

import (
	"encoding/json"

	"github.com/syndtr/goleveldb/leveldb"
)

func putJSON(db *leveldb.DB, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return db.Put([]byte(key), raw, nil)
}

func getJSON(db *leveldb.DB, key string, out any) error {
	raw, err := db.Get([]byte(key), nil)
	if err != nil {
		return err
	}
	return unmarshalJSON(raw, out)
}

func unmarshalJSON(raw []byte, out any) error {
	return json.Unmarshal(raw, out)
}
