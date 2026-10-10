# Four files added so that go-zenon runs in a browser

Nothing here is part of the `sim` module, and nothing here changes a line of anyone's
code. Each file is **added** to a package, at build time, in a copy under `.build/`:

| File | Added to | By | Why |
|---|---|---|---|
| `go_zenon_db_memory.go` | go-zenon `common/db` | `scripts/fetch-go-zenon.sh`, after the tree check | `NewLevelDBManagerInMemory`: go-zenon's own LevelDB manager, the one a node runs, opened on goleveldb's in-memory storage instead of on a directory. A browser has no disk. go-zenon has a manager that needs none, `memdbManager`, and it is no use here: it stacks a layer per momentum and never flattens them, so 400 momentums took 33 s and each new one was slower than the last, where this makes 4,000 in 13 s at a steady rate (measured 2026-10-09, natively) |
| `go_zenon_cache_memory.go` | go-zenon `chain/cache/storage` | the same | `NewCacheDBManagerInMemory`: the same for the chain's second database |
| `goleveldb_file_storage_js.go` | `github.com/syndtr/goleveldb/leveldb/storage` | `scripts/fetch-go-deps.sh` | The version go-zenon pins has no `js` build of three functions its file storage needs, so the package does not compile for `GOOS=js`. With the two files above the chain never opens a file: the functions have to exist, not work |
| `secp256k1_nocgo.go` | `github.com/ethereum/go-ethereum/crypto/secp256k1` | `scripts/fetch-go-deps.sh` | That package is a cgo binding to libsecp256k1, and without cgo it has no `Sign`, `RecoverPubkey`, `CompressPubkey` or `DecompressPubkey`. go-zenon's bridge and swap contracts and its peer discovery call them. The file defines them on the pure Go secp256k1 that go-ethereum itself falls back to (`crypto/signature_nocgo.go`) |

The first two could not live outside go-zenon, because the managers' fields are
unexported. The last two could not be done with `go build -overlay`, which refuses
to touch the module cache; so the two modules are copied out of it, at the versions
go-zenon pins, and `sim/go.mod` points at the copies with `replace`.

## What that means for what the page runs

- **The ptlc contract is go-zenon's, unchanged.** The tree is checked against
  `go-zenon/PIN` before anything is added, and nothing in it is edited afterwards.
- **The contract's own checks do not pass through any of these files.** It verifies
  locks with `btcec` and `crypto/ed25519`, pure Go on every platform.
- **The database is LevelDB with a different storage under it**, not a different
  database: the versioning, the rollback patches and the caches are the node's.
- **The bridge and swap contracts verify secp256k1 signatures with a different
  implementation of the curve than a node does.** They are in the build because the
  virtual machine links every embedded contract; nothing on the page calls them, and
  the devnet's genesis gives the swap contract no entries.
- **go-ethereum's pure Go signing path pins `btcec` at v2.3.2 for the whole module.**
  From v2.3.3 `ecdsa.SignCompact` returns one value and that file no longer compiles.
