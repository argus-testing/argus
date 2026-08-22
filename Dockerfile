FROM node:22-bookworm-slim AS frontend
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-bookworm AS go-build
WORKDIR /src/argus
COPY argus/go.mod argus/go.sum ./
RUN go mod download
COPY argus/ ./
RUN go build -o /out/argus ./cmd/argus \
    && go build -o /out/argus-mcp ./cmd/argus-mcp \
    && go build -o /out/playwright github.com/mxschmitt/playwright-go/cmd/playwright

FROM ubuntu:noble
WORKDIR /app
COPY --from=go-build /out/argus /out/argus-mcp /out/playwright /usr/local/bin/
COPY --from=frontend /app/argus/static ./argus/static
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && /usr/local/bin/playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/*
ENV ARGUS_DB_PATH=/app/data/argus.db PORT=8000
EXPOSE 8000
ENTRYPOINT ["/usr/local/bin/argus"]
