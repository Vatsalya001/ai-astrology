# astro-service — deterministic Vedic astrology computation.
#
# DEPENDENCY POLICY: this image must never gain a language-model SDK.
# See pyproject.toml and tests/test_no_llm_imports.py. The service is
# also offline by design — it makes no outbound calls at all, which is
# why no CA bundle or network tooling is installed beyond the base.

FROM python:3.12-slim-bookworm AS base

# uv, copied from its official distroless image. Pinned by digest-less
# tag deliberately: this is a build tool, not a runtime dependency.
COPY --from=ghcr.io/astral-sh/uv:0.5 /uv /uvx /bin/

ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1

WORKDIR /app

# ─── Dependencies ────────────────────────────────────────────────────
# Copied and installed before application code so that a source change
# does not invalidate the dependency layer.
COPY pyproject.toml uv.lock ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-install-project --no-dev

# ─── The ephemeris kernel ────────────────────────────────────────────
#
# Before the application code, because de421.bsp changes approximately
# never and the source changes constantly — this way a code edit does
# not re-copy 17 MB.
#
# It MUST be in the image. This service never downloads at runtime: it
# has no HTTP client by design (ADR-001), and an ephemeris fetched at
# boot would make chart computation depend on a network this service is
# not allowed to reach.
#
# Its absence was invisible for an entire phase. The container starts,
# answers /health, and passes every compose health check — it only fails
# on the first request that actually needs a planet, with
# "FileNotFoundError: ephemeris kernel missing at /app/data/de421.bsp".
# Nothing caught it until an end-to-end test computed a real chart,
# which is the whole argument for having one.
COPY data ./data

# ─── Application ─────────────────────────────────────────────────────
COPY app ./app
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev

ENV PATH="/app/.venv/bin:$PATH"

# Run as a non-root user. A compute service with no data access has
# little to steal, but a container escape is cheaper to mitigate than
# to explain.
RUN useradd --create-home --uid 10001 astro \
    && chown -R astro:astro /app
USER astro

EXPOSE 8100

# The healthcheck uses urllib from the stdlib rather than curl, so the
# image needs no extra packages.
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8100/health', timeout=2).status==200 else 1)"

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8100"]
