#!/usr/bin/env bash
#
# Every extracted Go toolchain file, re-hashed against the zip it came from.
#
# ── Why this exists ──
#
# `check-integrity.sh` covers tracked files. `npm ci` covers node_modules,
# because package-lock.json carries integrity hashes. **Nothing covered the
# Go module cache**, and docs/PROJECT_STATUS.md said so in a table after a
# single-bit flip in node_modules/lucide-react silently stopped 170 tests
# from running.
#
# On 2026-10-03 that gap produced the seventeenth corruption event on this
# machine, and the worst-shaped one so far:
#
#   src/internal/fuzz/fuzz.go        5 single-bit flips  ')' -> '('
#   pkg/tool/linux_amd64/asm         3 single-bit flips  (the ASSEMBLER)
#   pkg/tool/linux_amd64/cgo         6 flips, 5 single-bit
#
# Only the source file announced itself, as a syntax error inside the Go
# standard library. The two binaries are build TOOLS: a flipped bit in the
# assembler emits wrong machine code with nothing to notice, and every
# object it produced afterwards inherits that silently. That is the failure
# this script exists to find, because no test can.
#
# ── How it works ──
#
# Go extracts each toolchain from a zip in the download cache and verifies
# that zip's hash at download time. It never re-verifies the extracted
# tree. The zip is still on disk, so it is the reference: compare every
# extracted file against its zip entry.
#
# The zip's own CRCs are checked first. Repairing from a corrupt source
# would be worse than not repairing.
#
# ── Usage ──
#
#   scripts/check-toolchain.sh            # report
#   scripts/check-toolchain.sh --repair   # rewrite mismatched files from the zip
#
# `--repair` rewrites in place rather than deleting the tree, so a flip in
# one file does not cost a re-extraction of eleven thousand.

set -euo pipefail

REPAIR=0
[[ "${1:-}" == "--repair" ]] && REPAIR=1

# ── Resolving the toolchain from INSIDE the Go module ──
#
# This is load-bearing and it was wrong in the first version of this
# script. Run from the repository root, `go version` answers go1.22.2 —
# the system install, which ADR-007 exists because of. Run from
# services/api, it answers go1.26.8, the toolchain go.mod pins and the one
# every build actually uses.
#
# So the version has to be resolved from the module directory. Checking
# the system Go reported "no toolchain zip" and exited 0, which is a
# check that passes by looking at the wrong thing — the same
# measurement-under-the-wrong-heading mistake this repo has already made
# twice with provider factories.
MODULE_DIR="${MODULE_DIR:-}"
if [[ -z "$MODULE_DIR" ]]; then
    REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
    MODULE_DIR="$(dirname "$(find "$REPO_ROOT" -name go.mod -not -path '*/node_modules/*' \
        -print -quit)")"
fi
if [[ ! -f "$MODULE_DIR/go.mod" ]]; then
    echo "✗ no go.mod found; set MODULE_DIR to the Go module directory" >&2
    exit 1
fi

cd "$MODULE_DIR"

MODCACHE="$(go env GOMODCACHE)"
if [[ -z "$MODCACHE" || ! -d "$MODCACHE" ]]; then
    echo "✗ cannot locate GOMODCACHE" >&2
    exit 1
fi

TOOLCHAIN="$(go version | awk '{print $3}')"

python3 - "$MODCACHE" "$TOOLCHAIN" "$REPAIR" <<'PY'
import glob
import hashlib
import os
import stat
import sys
import zipfile

modcache, toolchain, repair = sys.argv[1], sys.argv[2], sys.argv[3] == "1"

# go1.26.8 -> v0.0.1-go1.26.8.linux-amd64, but let the glob find the
# platform suffix rather than assuming linux-amd64.
pattern = os.path.join(
    modcache, "cache", "download", "golang.org", "toolchain", "@v",
    f"v0.0.1-{toolchain}.*.zip")
zips = sorted(glob.glob(pattern))

if not zips:
    # Not a failure. A Go installed from a tarball rather than fetched as
    # a module has no zip to compare against, and this check simply does
    # not apply — saying so beats exiting non-zero on a healthy machine.
    print(f"– no toolchain zip in the module cache for {toolchain}")
    print("  (this check applies to a toolchain Go fetched as a module;")
    print("   a tarball or distro install has nothing to compare against)")
    sys.exit(0)

archive = zips[0]
print(f"toolchain {toolchain}  (as resolved inside the Go module)")
print(f"reference  {os.path.relpath(archive, modcache)}")

zf = zipfile.ZipFile(archive)

damaged_entry = zf.testzip()
if damaged_entry is not None:
    print(f"✗ the reference zip itself fails its CRC at {damaged_entry}")
    print("  Not repairing from it. Re-download:")
    print(f"    rm {archive} && go build ./...")
    sys.exit(1)

names = [n for n in zf.namelist() if not n.endswith("/")]
print(f"checking   {len(names)} files\n")

mismatched = []
missing = 0

for name in names:
    path = os.path.join(modcache, name)
    if not os.path.exists(path):
        missing += 1
        continue

    expected = zf.read(name)
    with open(path, "rb") as handle:
        found = handle.read()
    if expected == found:
        continue

    if len(expected) != len(found):
        mismatched.append((name, None))
        continue

    diffs = [(i, expected[i], found[i])
             for i in range(len(expected)) if expected[i] != found[i]]
    mismatched.append((name, diffs))

if not mismatched:
    suffix = f" ({missing} not extracted)" if missing else ""
    print(f"✓ every extracted toolchain file matches the zip byte for byte{suffix}")
    sys.exit(0)

print(f"✗ {len(mismatched)} file(s) differ from the zip they were extracted from\n")

for name, diffs in mismatched:
    short = name.split("/", 1)[-1]
    if diffs is None:
        print(f"  {short}: length differs")
        continue

    single_bit = sum(1 for _, a, b in diffs if bin(a ^ b).count("1") == 1)
    print(f"  {short}")
    print(f"    {len(diffs)} differing byte(s), {single_bit} of them a single bit")
    for offset, good, bad in diffs[:4]:
        print(f"      offset {offset}: {good:#04x} -> {bad:#04x}  "
              f"XOR {good ^ bad:#04x}")
    if len(diffs) > 4:
        print(f"      … and {len(diffs) - 4} more")

    # Said explicitly, because it changes what the finding means. Bytes
    # that each differ by exactly one bit are memory or storage
    # corruption; an edit changes bytes wholesale.
    if single_bit == len(diffs):
        print("    → every difference is one bit: memory or storage corruption,")
        print("      not an edit")

if not repair:
    print("\nRe-run with --repair to rewrite these from the zip.")
    sys.exit(1)

print("\nrepairing from the zip")
for name, _ in mismatched:
    path = os.path.join(modcache, name)
    expected = zf.read(name)

    # Module cache files are read-only by design. Restore the mode after
    # writing rather than leaving the tree writable.
    mode = stat.S_IMODE(os.stat(path).st_mode)
    os.chmod(path, mode | stat.S_IWUSR)
    with open(path, "wb") as handle:
        handle.write(expected)
    os.chmod(path, mode)

    with open(path, "rb") as handle:
        after = hashlib.sha256(handle.read()).hexdigest()
    ok = after == hashlib.sha256(expected).hexdigest()
    print(f"  {'✓' if ok else '✗'} {name.split('/', 1)[-1]}")

print("\n⚠ Clear the build cache as well:  go clean -cache")
print("  If the ASSEMBLER or a compiler was among the repaired files, every")
print("  cached object it produced may hold wrong machine code. Nothing")
print("  about those objects looks wrong, and no test will find them.")
PY
