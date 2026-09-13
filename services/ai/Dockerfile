# ai-service — LLM orchestration, RAG, safety and evaluation.
#
# Connects to Postgres with a READ-ONLY role. api-service is the sole
# writer (ADR-001).
#
# Refuses to start on a non-paid model provider when ENV=production
# (app/guards.py). That is deliberate: requests carry birth data and
# personal conversation content, and most free tiers may train on inputs.

FROM python:3.12-slim-bookworm AS base

COPY --from=ghcr.io/astral-sh/uv:0.5 /uv /uvx /bin/

ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1

WORKDIR /app

COPY pyproject.toml uv.lock ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-install-project --no-dev

COPY app ./app
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev

ENV PATH="/app/.venv/bin:$PATH"

RUN useradd --create-home --uid 10002 aisvc \
    && chown -R aisvc:aisvc /app
USER aisvc

EXPOSE 8200

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8200/health', timeout=2).status==200 else 1)"

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8200"]
