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
# ── --gate: why this can now run on a dirty tree ──
#
# This script used to be CI-and-clean-trees only, because on a working
# tree it flags every file you are editing and "cannot tell an edit from
# a bit flip". That had a fatal dependency: CI was the only place it ran,
# and CI stopped running on 2026-09-16. On 2026-09-21 the ephemeris
# kernel was corrupted again and reached a puzzling test failure rather
# than a checksum, because the one thing that looks for this had not
# executed in five days.
#
# `--gate` splits the mismatches instead of skipping any:
#
#   differs, and git REPORTS it modified  -> an edit. Listed, not fatal.
#   differs, and git says NOTHING         -> git could not see it. FATAL.
#
# The second bucket is the corruption signature, because git decides
# "unchanged" from stat before it will hash — so a file whose bytes
# changed underneath it is exactly the file git stays silent about. That
# silence is the signal, and it is what happened to de421.bsp: `git
# status` printed nothing while `git hash-object` disagreed.
#
# ── What this mode CANNOT do, stated plainly ──
#
# Corruption that arrives through a write() — anything that updates
# ctime — IS visible to git, lands in the "edit" bucket, and does not
# fail the gate. Attempting to simulate a bit flip by rewriting the file
# demonstrates exactly that: git notices, and `--gate` correctly calls
# it an edit.
#
# So `--gate` detects the class of corruption actually observed here
# (silent, in place, git blind) and cannot detect corruption
# indistinguishable from an edit. The full mode — no flag, clean tree,
# CI — remains the stronger check and is still the one CI runs. This
# mode exists so that SOMETHING runs when CI does not.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

gate=0
if [ "${1:-}" = "--gate" ]; then
  gate=1
fi

# Git's own view of what differs BETWEEN THE INDEX AND THE WORKTREE,
# stat cache and all. A silently corrupted file is absent from this list
# by construction — that absence is the whole discriminator.
#
# `git diff --cached` is deliberately NOT consulted, and including it was
# a real bug: it compares the index against HEAD, which says nothing
# about whether the bytes on disk match the index. After `git add`, the
# index holds the staged content, so a subsequent in-place corruption is
# a genuine index-vs-disk mismatch — while `--cached` lists the file as
# "changed" and made it immune. One `git add -A` blinded the whole gate.
known_edits="$(git diff --name-only | sort -u)"

is_known_edit() {
  printf '%s\n' "$known_edits" | grep -Fxq "$1"
}

silent=0
edits=0
silent_paths=""

while IFS=$'\t' read -r -d '' meta path; do
  mode="${meta%% *}"
  rest="${meta#* }"
  indexhash="${rest%% *}"

  # 120000 is a symlink and 160000 a gitlink. `git hash-object` FOLLOWS a
  # symlink and hashes the target's contents, while the index holds the
  # hash of the link's target-PATH string — so every tracked symlink
  # would report as a silent mismatch forever, hard-failing the gate with
  # advice that cannot clear it (`rm` + checkout restores an identical
  # link). The first symlink committed here would have made the gate
  # unusable, and an unusable gate gets deleted.
  case "$mode" in
    120000 | 160000) continue ;;
  esac

  [ -f "$path" ] || continue
  disk="$(git hash-object -- "$path")"
  [ "$disk" = "$indexhash" ] && continue

  if is_known_edit "$path"; then
    edits=$((edits + 1))
    [ "$gate" -eq 1 ] || printf '  (edit) %s\n' "$path"
  else
    silent=$((silent + 1))
    silent_paths="${silent_paths}${path}"$'\n'
    printf '  SILENT MISMATCH  %s\n    index %s\n    disk  %s\n' \
      "$path" "$indexhash" "$disk"
  fi
# `-z` because without it git C-quotes any path with non-ASCII or
# unusual bytes ("sch\303\266n.txt"), and the quoted name does not exist
# on disk — so `[ -f "$path" ]` failed and the file was skipped SILENTLY.
# A corrupted file with an accented name was simply never checked.
done < <(git ls-files -s -z)

if [ "$silent" -eq 0 ]; then
  if [ "$gate" -eq 1 ]; then
    echo "✓ no silent mismatches ($edits edited file(s) skipped, as intended)"
    exit 0
  fi
  if [ "$edits" -eq 0 ]; then
    echo "✓ every tracked file matches the index byte for byte"
    exit 0
  fi
  echo
  echo "$edits file(s) differ and git reports every one of them as modified."
  echo "That is ordinary uncommitted work, not corruption."
  exit 1
fi

echo
echo "$silent file(s) differ from the index while git reports them CLEAN."
echo
echo "That is not an edit — git lists edits. The bytes on disk are"
echo "damaged and git cannot see it. Restore each with:"
echo
echo "    rm <path> && git checkout -- <path>"
echo
echo "A plain 'git checkout' will NOT do it: git believes the file is"
echo "already correct, so the checkout is a no-op."
echo
echo "Then run memtest86+. Two single-bit flips were found in"
echo "services/astro/data/de421.bsp on 2026-09-21 and two more in a"
echo "golden dasha fixture on 2026-09-18. This machine has a history."
exit 1
