# ADR-007 — Pin the Go toolchain in go.mod

**Status:** accepted · 2026-09-13

## Decision

`services/api/go.mod` declares `toolchain go1.23.4`, so the Go tool fetches a verified
toolchain into the module cache rather than using whatever is installed system-wide.

## Context

Discovered during Phase 0 setup: the system Go 1.22.2 installation on a development
machine had a **single corrupted byte** in the standard library.

```
/usr/lib/go-1.22/src/time/time.go:1618:1: invalid character U+0008
```

Line 1618 began with `0x08` (backspace) where it should have been `0x09` (tab) — a
single-bit flip, `00001001` → `00001000`, in a file untouched on disk since March 2024.
Textbook bit-rot. Exactly one file in the entire stdlib was affected.

Every build importing `time` failed, which is effectively every build.

## Reason

- **Reproducibility.** Builds no longer depend on the state of a system package.
- **Verification.** Toolchains fetched by the Go tool are checksum-verified through the
  module proxy; a distribution package on disk is not re-verified after installation.
- **No privilege needed.** Repairing the system install required root. Pinning the
  toolchain did not, which unblocked the build immediately.
- **Consistency.** Every developer and CI runner uses an identical toolchain regardless
  of their distribution's packaging.

## Tradeoffs

- First build downloads ~70MB (cached thereafter)
- The pinned version needs periodic bumping for security fixes

## Notes

The corrupted file is unrelated to this project and remains on the affected machine.
Repair it independently with:

```bash
sudo apt-get install --reinstall golang-1.22-src
```

A single bit-flip is usually a fluke. Repeated occurrences would warrant `memtest86+`
and `smartctl -a` on the disk.
