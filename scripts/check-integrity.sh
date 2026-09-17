#!/usr/bin/env bash
#
# Every tracked file's bytes, re-hashed against the index.
#
# ── Why `git status` was not enough ──
#
# Observed on 2026-09-18, and the reason this file exists. Two tracked
# files held wrong bytes on disk while git reported a clean tree:
#
#   services/astro/data/de421.bsp                     the JPL kernel
#   tests/fixtures/.../017-.../expected_dasha.json    a golden file
#
# For both, `git status` and `git diff` printed nothing, and
# `git update-index --really-refresh` did not list them — while
# `git hash-object` disagreed with the index. Restoring needed
# `rm` first, because `git checkout` had nothing it believed was wrong.
#
# The likely mechanism is git's stat cache: it decides "unchanged" from
# size and mtime before it will hash. That is an inference, not
# something reproduced on demand — rewriting a file to plant a flipped
# bit changes ctime, and git then does notice. What is certain is the
# observation above, and that re-hashing is what found it.
#
# The golden file had two single-bit flips 220 bytes apart — 0x20 -> 0x21
# and 0x2D -> 0x25 — which is the signature of memory corruption rather
# than of an edit. Both surfaced only as puzzling test failures.
#
# The repo notes nine data-corruption events and an unrun memtest86+.
# This is how the next one gets found.
#
# Exit 1 on any mismatch that is not an intentional edit, so CI can gate
# on it. Locally, the files you are working on will show up too — that is
# correct and unavoidable: this cannot tell an edit from a bit flip. Use
# it on a clean tree.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

mismatches=0
while read -r _mode indexhash _stage path; do
  [ -f "$path" ] || continue
  disk="$(git hash-object "$path")"
  if [ "$disk" != "$indexhash" ]; then
    printf '  %s\n    index %s\n    disk  %s\n' "$path" "$indexhash" "$disk"
    mismatches=$((mismatches + 1))
  fi
done < <(git ls-files -s)

if [ "$mismatches" -eq 0 ]; then
  echo "✓ every tracked file matches the index byte for byte"
  exit 0
fi

echo
echo "$mismatches file(s) differ from the index."
echo
echo "If you edited them, that is this script working as intended — it"
echo "cannot distinguish an edit from a flipped bit. If you did NOT edit"
echo "one of them, the bytes on disk are damaged and git cannot see it:"
echo
echo "    rm <path> && git checkout -- <path>"
echo
echo "A plain 'git checkout' will not do it — git believes the file is"
echo "already correct. And run memtest86+."
exit 1
