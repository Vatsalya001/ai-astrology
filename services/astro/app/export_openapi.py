"""Emit the OpenAPI document to stdout.

This is the source of the cross-service contract. Go clients are
GENERATED from it (ADR-005) — never hand-written, because a hand-written
client drifts silently from the contract it claims to implement.

`sort_keys=True` matters: without it FastAPI's output reorders between
runs and every regeneration produces a noisy diff, which defeats the CI
check that detects real contract changes.

Usage:  uv run python -m app.export_openapi > contracts/openapi/<svc>.json
"""

import json
import sys

from app.main import app


def main() -> None:
    json.dump(app.openapi(), sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
