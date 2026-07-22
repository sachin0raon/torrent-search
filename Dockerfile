# syntax=docker/dockerfile:1

# ---- Stage 1: build the React (Vite) SPA ----
FROM node:20-alpine AS frontend
WORKDIR /fe
# Install deps first for layer caching.
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci
COPY frontend/ ./
RUN npm run build            # -> /fe/dist

# ---- Stage 2: install backend deps into a venv ----
FROM python:3.11-slim AS backend
WORKDIR /app
# uv gives fast, reproducible installs.
RUN pip install --no-cache-dir uv
ENV VIRTUAL_ENV=/opt/venv
RUN uv venv "$VIRTUAL_ENV"
# Copy project metadata + source, then install the package (and its deps) into the venv.
COPY backend/pyproject.toml ./
COPY backend/app ./app
RUN uv pip install --python "$VIRTUAL_ENV/bin/python" .

# ---- Stage 3: minimal runtime ----
FROM python:3.11-slim AS runtime

# Non-root user.
RUN groupadd -g 1001 appgroup \
    && useradd -u 1001 -g appgroup -m -s /usr/sbin/nologin appuser

WORKDIR /app
ENV PATH="/opt/venv/bin:$PATH" \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    LOG_LEVEL=info \
    STATIC_DIR=/app/static \
    CONFIG_JSON_PATH=/data/config.json

# Runtime deps (venv) + application source + built frontend.
COPY --from=backend /opt/venv /opt/venv
COPY --chown=appuser:appgroup backend/app ./app
COPY --from=frontend --chown=appuser:appgroup /fe/dist ./static

# Writable location for the persisted forum-base-url override.
RUN mkdir -p /data && chown appuser:appgroup /data
VOLUME ["/data"]

USER 1001
EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8000/api/health').status==200 else 1)"

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
