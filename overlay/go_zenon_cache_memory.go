// This file is not part of the sim module. scripts/fetch-go-zenon.sh adds it
// to go-zenon's chain/cache/storage after the fetched tree has been checked
// against its pin (README.md beside this file).
//
// It is NewCacheDBManager with one line different: the node's own cache
// manager, opened on goleveldb's in-memory storage instead of on a directory,
// for the same reason as common/db/manager_memory.go.

package storage

import (
	"github.com/syndtr/goleveldb/leveldb"
	ldbstorage "github.com/syndtr/goleveldb/leveldb/storage"

	"github.com/zenon-network/go-zenon/common"
)

func NewCacheDBManagerInMemory() CacheManager {
	db, err := leveldb.Open(ldbstorage.NewMemStorage(), nil)
	common.DealWithErr(err)
	return &cacheManager{
		ldb: db,
		write: func(batch *leveldb.Batch) error {
			return db.Write(batch, nil)
		},
	}
}
