"""The suite is not allowed to leave this machine.

PHASE-04 §17: *"MockProvider powers all CI; no CI job makes a network
call."* That was verified once, by running the suite under an ad-hoc
plugin that was never committed — so the claim was true on the day
somebody checked it and enforced by nothing afterwards. This file is the
enforcement.

── Why it matters more than it used to ──

`services/ai/.env` now holds a real provider key, and pydantic-settings
reads that file at import. A test that builds a provider without
monkeypatching `settings` no longer talks to a fixture: it talks to the
vendor, with a real key, and spends a real budget. On a free tier that
budget is ~142 calls a day, so a single careless test can exhaust the
quota the accuracy measurement needs.

The failure is also silent in the direction that matters. A test making
a live call still PASSES — faster or slower, but green — so nothing
reports that CI has started depending on a third party being up, on a
key being valid, and on a model's mood.

── Loopback is allowed, deliberately ──

Blocking every connection would break tests that need one locally:
`test_a_dead_primary_is_served_by_the_fallback` connects to a
deliberately closed port to reproduce a stopped Ollama, and it must
receive a real `ConnectionRefusedError` rather than this guard's
exception — otherwise it exercises the guard instead of the fallback.

The rule is therefore "no EXTERNAL call", which is what §17 actually
asks for.
"""

from __future__ import annotations

import ipaddress
import socket
from collections.abc import Iterator
from typing import Any

import pytest


class ExternalNetworkCallError(RuntimeError):
    """Raised instead of opening a connection off this machine."""


def _is_loopback(host: object) -> bool:
    if not isinstance(host, str) or not host:
        return False
    if host in ("localhost", "localhost.localdomain", "::1"):
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        # A hostname that is not an IP literal. Not resolved here on
        # purpose: resolving it would itself be a network call, and a
        # name that needs DNS is not loopback by any useful definition.
        return False


def _refuse(where: str, address: object) -> ExternalNetworkCallError:
    return ExternalNetworkCallError(
        f"{where} tried to open a connection to {address!r}.\n\n"
        f"No test in this suite may call a third party. PHASE-04 §17 requires CI to "
        f"run entirely on MockProvider, and services/ai/.env now carries a REAL "
        f"provider key that pydantic reads at import — so an unmocked provider "
        f"spends real quota against a real vendor, and still reports PASS.\n\n"
        f"Use MockProvider, or monkeypatch the transport. If this test genuinely "
        f"needs a socket, point it at loopback."
    )


@pytest.fixture(autouse=True)
def _no_external_network(monkeypatch: pytest.MonkeyPatch) -> Iterator[None]:
    """Autouse, so a new test file is covered without opting in.

    Opt-in enforcement is enforcement that the next file forgets.
    """
    real_connect = socket.socket.connect
    real_connect_ex = socket.socket.connect_ex
    real_create = socket.create_connection

    def guarded_connect(self: socket.socket, address: Any) -> Any:
        host = address[0] if isinstance(address, tuple) else address
        if not _is_loopback(host):
            raise _refuse("socket.connect", address)
        return real_connect(self, address)

    def guarded_connect_ex(self: socket.socket, address: Any) -> Any:
        host = address[0] if isinstance(address, tuple) else address
        if not _is_loopback(host):
            raise _refuse("socket.connect_ex", address)
        return real_connect_ex(self, address)

    def guarded_create(address: Any, *args: Any, **kwargs: Any) -> Any:
        host = address[0] if isinstance(address, tuple) else address
        if not _is_loopback(host):
            raise _refuse("socket.create_connection", address)
        return real_create(address, *args, **kwargs)

    monkeypatch.setattr(socket.socket, "connect", guarded_connect)
    monkeypatch.setattr(socket.socket, "connect_ex", guarded_connect_ex)
    monkeypatch.setattr(socket, "create_connection", guarded_create)
    yield
