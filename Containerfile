# syntax=docker/dockerfile:1.7
# Builder stage — installs deps with uv into /app/.venv
FROM python:3.13-slim AS builder

ENV UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1 \
    UV_PYTHON_DOWNLOADS=never \
    PYTHONDONTWRITEBYTECODE=1

COPY --from=ghcr.io/astral-sh/uv:0.5.13 /uv /uvx /usr/local/bin/

WORKDIR /app

# Install deps first (cached layer) then project
COPY pyproject.toml uv.lock README.md ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev --no-install-project

COPY src ./src
COPY migrations ./migrations
COPY alembic.ini ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev


# Runtime stage — minimal, OpenShift-friendly (random UID, group 0)
FROM python:3.13-slim AS runtime

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PATH="/app/.venv/bin:$PATH"

WORKDIR /app

COPY --from=builder /app /app

# OpenShift-compatible permissions: group 0, group-writable
# The container will run with a random UID; the GID 0 group is always present.
RUN chgrp -R 0 /app && chmod -R g=u /app && \
    mkdir -p /etc/riptide-collector && \
    chgrp 0 /etc/riptide-collector && chmod g=u /etc/riptide-collector

EXPOSE 8000

USER 1001

CMD ["uvicorn", "riptide_collector.main:app", "--host", "0.0.0.0", "--port", "8000"]
