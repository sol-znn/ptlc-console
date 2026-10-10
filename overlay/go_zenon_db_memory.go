// This file is not part of the sim module. scripts/fetch-go-zenon.sh adds it
// to go-zenon's common/db after the fetched tree has been checked against its
// pin (README.md beside this file).
//
// A node keeps its chain in LevelDB on disk, through ldbManager. go-zenon
// also has a manager that needs no disk, memdbManager; it stacks one layer
// per momentum and never flattens them, so every read walks the whole history
// and a chain of a few hundred momentums already takes seconds for each new
// one. A browser has no disk and the page's chain runs for hours.
//
// This is NewLevelDBManager with one line different: the same ldbManager, the
// one a node runs, opened on goleveldb's in-memory storage instead of on a
// directory. Its fields are unexported, which is the only reason this cannot
// live outside the package.

package db

import (
	lru "github.com/hashicorp/golang-lru"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"

	"github.com/zenon-network/go-zenon/common"
)

func NewLevelDBManagerInMemory() Manager {
	ldb, err := leveldb.Open(storage.NewMemStorage(), nil)
	common.DealWithErr(err)
	l1Cache, err := lru.New(l1CacheSize)
	common.DealWithErr(err)
	l2Cache, err := lru.New(l2CacheSize)
	common.DealWithErr(err)
	return &ldbManager{
		location: "in-memory",
		l1Cache:  l1Cache,
		l2Cache:  l2Cache,
		ldb:      ldb,
		write: func(batch *leveldb.Batch) error {
			return ldb.Write(batch, nil)
		},
	}
}
