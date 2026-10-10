#!/bin/sh
# Builds the chain and the scenarios into web/src/sim/gen/: ptlc.wasm, and the
# wasm_exec.js of the Go that compiled it (the two must match, so the glue is
# copied from this toolchain and never checked in).
#
# Needs Go 1.23 or later and git. Fetches go-zenon and two of its
# dependencies into .build/ first; see go-zenon/PIN and overlay/README.md.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
sh "$root/scripts/fetch-go-zenon.sh"
sh "$root/scripts/fetch-go-deps.sh"
. "$root/go-zenon/PIN"

out="$root/web/src/sim/gen"
mkdir -p "$out"
cd "$root/sim"
# -s -w drops the symbol table and DWARF, a third of the file. The paths in
# what is left are trimmed so that two machines build the same bytes.
GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.goZenonCommit=$BASE -X ptlc-console/sim/chain.GoZenonTree=$TREE" \
  -o "$out/ptlc.wasm" ./cmd/wasm

goroot=$(go env GOROOT)
if [ -f "$goroot/lib/wasm/wasm_exec.js" ]; then
  cp "$goroot/lib/wasm/wasm_exec.js" "$out/wasm_exec.js"
else
  # Go 1.23 and earlier kept it under misc/.
  cp "$goroot/misc/wasm/wasm_exec.js" "$out/wasm_exec.js"
fi
echo "sim: $(wc -c < "$out/ptlc.wasm") bytes, go-zenon $BASE"
