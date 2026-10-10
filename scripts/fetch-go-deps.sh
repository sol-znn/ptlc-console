#!/bin/sh
# Puts two of go-zenon's dependencies at .build/, each with one file added,
# where sim/go.mod looks for them. overlay/README.md says which files and
# why; in short, neither module compiles for the browser as pinned.
#
# Each is copied out of the module cache at the version go-zenon pins, byte
# for byte, and gains one file. Nothing that was there is changed.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$root/.build"

# place MODULE VERSION OUT-DIR ADDED-FILE AS-PATH
place() {
  module=$1 version=$2 out="$root/.build/$3" added="$root/overlay/$4" as=$5
  if [ -f "$out/.version" ] && [ "$(cat "$out/.version")" = "$version" ] && cmp -s "$added" "$out/$as"; then
    echo "$3: already $version with $4"
    return
  fi
  # Asked for by version from outside the sim module, so that the replace in
  # sim/go.mod, which points at the directory this makes, is not in the way.
  src=$(cd "$root/.build" && GOFLAGS=-mod=mod GOWORK=off go mod download -json "$module@$version" | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",*$/\1/p' | sed 's/\\/\//g')
  [ -d "$src" ] || { echo "$3: could not find $module@$version in the module cache" >&2; exit 1; }
  rm -rf "$out"
  mkdir -p "$out"
  cp -R "$src/." "$out/"
  chmod -R u+w "$out"
  cp "$added" "$out/$as"
  echo "$version" > "$out/.version"
  echo "$3: $module@$version + $as"
}

place github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 goleveldb \
  goleveldb_file_storage_js.go leveldb/storage/file_storage_js.go
place github.com/ethereum/go-ethereum v1.10.22 go-ethereum \
  secp256k1_nocgo.go crypto/secp256k1/secp256_nocgo.go
