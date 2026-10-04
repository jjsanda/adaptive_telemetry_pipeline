# syntax=docker/dockerfile:1
# Multi-stage build: resolve wheels in a builder, ship only the runtime.

FROM python:3.12-slim AS builder
WORKDIR /app
ENV PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1
COPY requirements.txt .
# Install into an isolated prefix we can copy verbatim into the final stage.
RUN pip install --prefix=/install -r requirements.txt

FROM python:3.12-slim AS runtime
# Run as a non-root user.
RUN groupadd --system app && useradd --system --gid app --home-dir /app app
WORKDIR /app
ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PORT=8083
# Bring in the dependencies resolved by the builder stage.
COPY --from=builder /install /usr/local
COPY --chown=app:app app ./app
USER app
EXPOSE 8083
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["python", "-c", "import os,urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:'+os.environ.get('PORT','8083')+'/healthz').status==200 else 1)"]
# app.main reads PORT (default 8083) and serves the ASGI app with uvicorn.
CMD ["python", "-m", "app.main"]
