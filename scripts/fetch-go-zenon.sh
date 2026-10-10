#!/bin/sh
# Puts the patched go-zenon at .build/go-zenon, where sim/go.mod looks for it.
#
# Fetches the one commit go-zenon/PIN names, applies any patch there is in
# go-zenon/patches in name order (there is none today), and refuses to go on
# unless the resulting tree is exactly the tree the pin names. The chain the
# page runs is then that branch, hash for hash, and not "whatever the branch
# pointed at on the day of the build".
#
# Two files are then added, after the check and outside the tree it covers:
# manager_memory.go in common/db and in chain/cache/storage, which open
# go-zenon's own two LevelDB managers on memory instead of on a directory
# (overlay/README.md). No file of the tree is changed.
#
# Fetches nothing when .build/go-zenon already holds that tree.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
. "$root/go-zenon/PIN"
out="$root/.build/go-zenon"
add() {
  cp "$root/overlay/go_zenon_db_memory.go" "$out/common/db/manager_memory.go"
  cp "$root/overlay/go_zenon_cache_memory.go" "$out/chain/cache/storage/manager_memory.go"
}

if [ -f "$out/.tree" ] && [ "$(cat "$out/.tree")" = "$TREE" ]; then
  add
  echo "go-zenon: .build/go-zenon is already tree $TREE"
  exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git init -q "$work"
# The patches carry the files as the branch has them; nothing here may
# renormalise a line ending on the way through.
git -C "$work" config core.autocrlf false
git -C "$work" remote add origin "$REPO"
git -C "$work" fetch -q --depth 1 origin "$BASE"
git -C "$work" checkout -q FETCH_HEAD
count=0
for p in $(find "$root/go-zenon/patches" -maxdepth 1 -name '*.patch' 2>/dev/null | sort); do
  git -C "$work" apply --index --whitespace=nowarn "$p"
  count=$((count + 1))
done
got=$(git -C "$work" write-tree)
if [ "$got" != "$TREE" ]; then
  echo "go-zenon: the patched tree is $got, go-zenon/PIN says $TREE: the patches and the pin have drifted" >&2
  exit 1
fi
rm -rf "$out"
mkdir -p "$out"
git -C "$work" checkout-index -a -f --prefix="$out/"
add
echo "$got" > "$out/.tree"
echo "go-zenon: $REPO @ $BASE + $count patches = tree $got"
