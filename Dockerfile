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

# ---- Stage 3: build the Go torrent-streaming service ----
FROM golang:1.25-bookworm AS gostream
WORKDIR /src
# Dependencies first for layer caching.
COPY streamer/go.mod streamer/go.sum ./
RUN go mod download
COPY streamer/ ./
# Pure-Go build (anacrolix's sqlite dep is modernc, so CGO is off) → static binary.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/streamer .

# ---- Stage 4: minimal runtime ----
FROM python:3.11-slim AS runtime

# nginx (front door), supervisor (process manager), ca-certificates (for the
# Go service's HTTPS tracker-list fetches).
RUN apt-get update \
    && apt-get install -y --no-install-recommends nginx supervisor ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Non-root user.
RUN groupadd -g 1001 appgroup \
    && useradd -u 1001 -g appgroup -m -s /usr/sbin/nologin appuser

WORKDIR /app
ENV PATH="/opt/venv/bin:$PATH" \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    LOG_LEVEL=info \
    STATIC_DIR=/app/static \
    CONFIG_JSON_PATH=/data/config.json \
    STREAM_DOWNLOAD_DIR=/downloads \
    STREAM_LISTEN_ADDR=127.0.0.1:8001

# Runtime deps (venv) + application source + built frontend + streamer binary.
COPY --from=backend /opt/venv /opt/venv
COPY --chown=appuser:appgroup backend/app ./app
COPY --from=frontend --chown=appuser:appgroup /fe/dist ./static
COPY --from=gostream /out/streamer /usr/local/bin/streamer

# Process config.
COPY deploy/nginx.conf /etc/nginx/nginx.conf
COPY deploy/supervisord.conf /etc/supervisord.conf

# Writable locations: persisted config (/data), ephemeral torrent data
# (/downloads), and nginx/supervisor scratch under /tmp — all owned by appuser.
RUN mkdir -p /data /downloads /tmp/nginx-client /tmp/nginx-proxy \
        /tmp/nginx-fastcgi /tmp/nginx-uwsgi /tmp/nginx-scgi \
    && chown -R appuser:appgroup /data /downloads /tmp/nginx-client \
        /tmp/nginx-proxy /tmp/nginx-fastcgi /tmp/nginx-uwsgi /tmp/nginx-scgi \
    && chmod +x /usr/local/bin/streamer
VOLUME ["/data", "/downloads"]

USER 1001
EXPOSE 8080

# Liveness via the nginx front door → FastAPI health route.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:8080/api/health').status==200 else 1)"

CMD ["supervisord", "-c", "/etc/supervisord.conf"]
