FROM node:22-bookworm-slim AS frontend
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM mcr.microsoft.com/playwright/python:v1.62.0-noble
WORKDIR /app
COPY --from=ghcr.io/astral-sh/uv:0.10.9 /uv /uvx /bin/
COPY pyproject.toml uv.lock README.md LICENSE ./
COPY argus ./argus
COPY --from=frontend /app/argus/static ./argus/static
RUN uv sync --frozen --no-dev
ENV PATH="/app/.venv/bin:$PATH" ARGUS_DATA_DIR=/app/data
EXPOSE 8000
CMD ["uvicorn", "argus.app:app", "--host", "0.0.0.0", "--port", "8000"]
