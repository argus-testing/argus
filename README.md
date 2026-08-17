# Argus

Argus is a local, open-source visual UI testing app. Describe a test, point it at an HTTP(S) page, and watch an agent inspect the page in an isolated Playwright browser context. Runs, timelines, screenshot references, and structured reports stay in local SQLite storage.

## Requirements

- Python 3.11+, [`uv`](https://docs.astral.sh/uv/), and Node.js 20.19+ or 22.12+
- A [Gemini API key](https://aistudio.google.com/app/apikey) for real runs

## Run locally

```bash
cp .env.example .env
# Set GEMINI_API_KEY in .env
uv sync --dev
uv run playwright install chromium
cd frontend && npm install && npm run build && cd ..
uv run uvicorn argus.app:app --reload --env-file .env
```

Open <http://localhost:8000>. For frontend hot reload, run `npm run dev` in `frontend/` alongside Uvicorn and open <http://localhost:5173>.

Configuration is environment-only:

| Variable | Default | Purpose |
| --- | --- | --- |
| `GEMINI_API_KEY` | — | Required for real execution |
| `GEMINI_MODEL` | `gemini-2.5-flash` | Gemini REST model |
| `ARGUS_DATA_DIR` | `data` | SQLite and screenshot directory |
| `ARGUS_HEADLESS` | `true` | Playwright browser mode |
| `ARGUS_RUN_TIMEOUT` | `300` | Run timeout in seconds |

Argus accepts normal HTTP(S) targets, including trusted localhost and private-network apps. It rejects credentials and sensitive query parameters in target URLs, and never stores provider secrets, typed browser values, or inspected page content. The settings screen only shows whether provider configuration is present.

## Docker

```bash
cp .env.example .env
# Set GEMINI_API_KEY in .env
docker compose up --build
```

The UI is available at <http://localhost:8000> and persistent data is written to `./data`.

## Development checks

```bash
uv run pytest
uv run ruff check argus tests
uv run pyright argus tests
cd frontend && npm run typecheck && npm run lint && npm run build
```

## Architecture

- `argus/runtime`: provider-neutral agent, message, tool, and session boundary
- `argus/providers/gemini.py`: raw `httpx` Gemini REST/SSE adapter (no provider SDK)
- `argus/pipeline.py`: planning, one browser agent, evidence capture, and reporting
- `argus/store.py`: SQLite runs, events, screenshots, and reports
- `argus/app.py`: REST API, reconnectable per-run WebSockets, and built UI serving
- `frontend`: dashboard/composer, live session, history, report, and read-only settings

API docs are available at `/docs`. All data is local; there is no authentication or multi-user isolation in this release.

## License

MIT
