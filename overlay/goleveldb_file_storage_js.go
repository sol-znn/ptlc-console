// This file is not part of the sim module. scripts/build-sim.sh lays it over
// github.com/syndtr/goleveldb/leveldb/storage with `go build -overlay`, where
// the pinned version has no js build of the three functions its file storage
// needs and so does not compile for the browser at all.
//
// The chain here is kept in go-zenon's in-memory database and never opens a
// file, so these are never called. They exist for the package to compile, and
// say so if that ever stops being true.

//go:build js

package storage

import (
	"errors"
	"os"
)

var errNoFiles = errors.New("goleveldb file storage is not available in a browser")

type jsFileLock struct{}

func (jsFileLock) release() error { return nil }

func newFileLock(path string, readOnly bool) (fileLock, error) { return nil, errNoFiles }

func rename(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

func isErrInvalid(err error) bool { return false }

func syncDir(name string) error { return errNoFiles }
