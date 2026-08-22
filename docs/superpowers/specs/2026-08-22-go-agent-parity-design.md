# Go Agent Parity Design

## Objective

Make the standalone Go edition of Argus capable of carrying out the same core browser-QA work as the private Python agent: understand a natural-language test, inspect and operate a real application, use visual grounding when semantic interaction fails, verify effects with fresh evidence, and return an honest structured verdict.

Parity here means browser-agent behavior and evidence quality. It does not include SaaS-only organization management, billing, schedules, integrations, admin controls, or historical Python agent-version compatibility.

## Product Contract

The Go edition must support:

- navigation within an explicitly authorized origin set;
- semantic inspection with stable element references rather than model-invented CSS selectors;
- clicking, typing, form filling and submission, option selection, keyboard input, scrolling, viewport changes, and condition-based waiting;
- screenshots that are actually returned to the active Gemini conversation;
- visual element location and coordinate clicking when semantic interaction is insufficient;
- browser console and bounded network-failure observations;
- authenticated tests with run-scoped secrets that are never persisted or emitted;
- state-changing tests only when the caller explicitly authorizes mutations;
- action-followed-by-observation evidence and conservative verdict normalization;
- isolated Playwright contexts and durable local reports, timelines, and screenshots.

The existing REST API, WebSocket timeline, MCP adapter, SQLite store, React interface, and staged pipeline remain the standalone application shell.

## Architecture

### Browser observation and element references

`browser.Session` will return a structured `PageSnapshot` containing the sanitized URL, title, bounded visible text, viewport, forms, and interactive elements. Every interactive element receives a short run-local reference such as `e1`. Each record includes safe attributes needed for reasoning: role/tag, accessible name, label, placeholder, input type, current non-secret value metadata, disabled/checked/selected state, and a bounded target description.

The browser session owns the reference-to-locator mapping. Agent tools accept element references rather than arbitrary selectors. References are refreshed on each inspection and invalid references fail explicitly. This both improves reliability and removes CSS-selector text matching as a safety mechanism.

### Tool surface

Explorer receives read-oriented tools plus safe navigation and scrolling. Executor receives the complete authorized interaction surface. Critic receives observation tools only.

The common tools are:

- `inspect_page`
- `screenshot`
- `navigate`
- `click`
- `type_text`
- `fill_form`
- `submit_form`
- `select_option`
- `press_key`
- `scroll`
- `resize_viewport`
- `wait_for`
- `console_errors`
- `network_errors`
- `find_elements`
- `visual_click`

High-level form helpers compose the lower-level browser session methods; they do not bypass policy enforcement.

### Multimodal tool results

The agent runtime will support a tool result containing both a JSON result and follow-up message parts. Screenshot, `find_elements`, and visual-interaction tools attach the current PNG to the same model conversation after the function result. Images are not encoded into persisted events or ordinary JSON reports.

Gemini requests preserve the function-call/function-response sequence and add the image as a user multimodal observation before the next model turn. Deterministic provider tests will assert the exact ordering and that image bytes never enter stored events.

### Visual grounding

`find_elements` captures the current viewport and asks Gemini to return bounded coordinates and confidence for the requested visual target. Coordinates are validated against the viewport. `visual_click` performs a fresh capture, requests grounding, requires the configured confidence floor, applies action policy, clicks the validated point, and returns a fresh snapshot plus screenshot.

Visual grounding is a fallback, not the default. Semantic references are attempted first. A per-agent-call visual budget prevents unbounded model usage.

### Authorization and secrets

Runs remain read-only by default. The create-run contract gains an optional authorization object:

```json
{
  "allow_mutations": false,
  "allow_destructive": false,
  "allowed_origins": ["https://app.example.test"],
  "secret_bindings": {
    "login_email": "user@example.test",
    "login_password": "..."
  }
}
```

The initial target origin is always authorized. Additional origins must be explicit and use HTTP(S). Loopback/private targets remain supported because local testing is a core feature, but model-driven navigation cannot leave the authorized set. Redirects are checked after navigation and rejected if they escape it.

Secret bindings live only in the in-memory active-run registry. SQLite stores the run URL, instructions, non-secret authorization flags, and allowed origins, but never secret names or values. The model refers to a binding by name when typing; the tool resolves it internally. Raw secret values are never included in model messages, tool events, errors, screenshot labels, or reports. Before screenshot capture, sensitive input values are temporarily covered in the rendered page and restored immediately afterward, so multimodal requests and persisted evidence cannot reveal them. Browser action events record only the element reference and a redacted value descriptor.

Mutating actions require `allow_mutations=true`. Clearly destructive actions additionally require `allow_destructive=true`; destructive authority is invalid unless mutation authority is also enabled. The policy engine classifies form submissions and clicks using the resolved element's tag, role, accessible name, form method, and nearby semantics. Policy is enforced below the model/tool layer so alternate selectors or coordinates cannot bypass it.

### Pipeline and evidence

The six stages remain Validator, Comprehender, Explorer, Strategist, Executor, and the final independent Critic. The README will describe that count consistently.

Structured stage outputs receive strict schemas and validation. Invalid outputs get one bounded repair attempt; otherwise the run becomes inconclusive rather than silently accepting malformed content.

Executor follows this invariant for significant actions:

1. capture the before snapshot;
2. perform one authorized action;
3. wait for page stability or an explicit condition;
4. capture the after snapshot and screenshot;
5. record the concrete delta used as evidence.

A passed result must cite at least one persisted screenshot reference or bounded structured observation for every requested test case. The Critic cannot upgrade an inconclusive executor result without fresh positive evidence. Unsupported, incomplete, or malformed passes normalize to inconclusive.

### Persistence and API compatibility

Existing clients that send only `url` and `instructions` continue to work as read-only runs. Schema migration adds non-secret authorization metadata without breaking existing SQLite files.

WebSocket and REST events remain bounded and redact inspected page text, typed values, headers, request bodies, and query secrets. Reports may contain model-written summaries, but a final redaction pass removes every active secret value before persistence.

MCP gains the optional non-secret policy flags but does not accept raw secrets over stdio in the first release. Authenticated MCP runs use environment-backed secret bindings configured on the Argus server.

## Error Handling

- Stale element references produce a retryable observation error and force re-inspection.
- Navigation outside authorized origins is rejected before the request; unauthorized redirects close the page and fail the step.
- Tool timeouts and detached elements are returned as bounded failures rather than crashing the pipeline.
- Visual matches below the confidence threshold do not click.
- Browser/provider/rate-limit/time-limit failures retain the current public error categories.
- Cancellation closes the run context, browser context, and outstanding visual request.
- A failed evidence capture prevents a pass verdict.

## Testing Strategy

All behavioral changes use red-green-refactor development.

Unit tests cover element reference creation, stale references, action policy, origin matching, redirect checks, secret resolution/redaction, structured stage validation, screenshot attachment ordering, coordinate validation, evidence gates, and event sanitization.

Runner tests use deterministic providers and fake browser sessions to exercise the complete pipeline without Gemini. Playwright integration tests run against a local fixture application containing search, forms, selects, scrolling, responsive layout, safe mutations, destructive controls, redirects, console errors, and delayed content.

An opt-in Gemini smoke test exercises semantic and visual paths when `GEMINI_API_KEY` is available. It is not required for offline CI.

Release verification runs:

```bash
go test -race ./...
go vet ./...
npm ci
npm test
npm run typecheck
npm run lint
npm run build
docker build .
```

The Go tests must not depend on prebuilt frontend output. Docker runtime smoke verification checks the UI, REST settings, one deterministic fixture run, screenshots, and cancellation.

## Packaging and Documentation

A `.dockerignore` excludes Git metadata, environment files, local data, caches, build output, and Python artifacts. `.gitignore` excludes Python bytecode and test caches left by the former implementation. The README describes supported tools, read-only defaults, authorization, secret bindings, visual grounding, limitations, and exact clean-clone commands without claiming unsupported behavior.

The license remains source-available and is described that way consistently. No claim will call the current ASAL release OSI open source before its MIT conversion.

## Acceptance Criteria

1. A clean checkout passes every offline verification command in the documented order.
2. The Docker image builds and serves the frontend/API with Chromium installed.
3. The fixture suite proves search typing, form fill/submit, select, scroll, keyboard, responsive resize, semantic click, and visual fallback.
4. Model-driven navigation cannot leave the authorized origin set, including through redirects.
5. Read-only runs cannot submit or mutate; authorized runs can; destructive actions require explicit destructive authority.
6. Secret values never appear in SQLite, events, logs, reports, or model requests.
7. Screenshots taken during a tool loop are supplied back to Gemini as images.
8. Every passed test case has positive, persisted evidence; unsupported passes become inconclusive.
9. Existing URL/instructions clients and existing SQLite databases continue to work.
10. No private Python source or SaaS-only subsystem is copied into the public repository.
