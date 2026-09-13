# ADR-007 — Pin the Go toolchain in go.mod

**Status:** accepted · 2026-09-13

## Decision

`services/api/go.mod` declares `toolchain go1.26.8`, so the Go tool fetches a verified
toolchain into the module cache rather than using whatever is installed system-wide.

**Stay one minor release behind the newest.** Go 1.27.1 was available and was tried
first; `govulncheck` — itself built against 1.26 — could not parse 1.27 standard-library
source and failed with parse errors rather than a vulnerability report. The analysis
tooling ecosystem (`govulncheck`, `golangci-lint`, `gopls`) reliably lags a major Go
release by weeks. Running on the latest patch of the previous line gets the security
fixes without breaking the tools that verify them.

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
