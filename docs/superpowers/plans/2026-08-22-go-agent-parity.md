# Go Agent Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give standalone Go Argus the private agent's essential browser interaction, multimodal grounding, authorization, secret safety, and evidence reliability.

**Architecture:** Keep the existing staged runner and add three focused boundaries: a policy package for origins and mutations, a structured browser session with stable element references, and multimodal tool outputs that feed fresh screenshots back into Gemini. REST creates runs with persisted non-secret policy and ephemeral secret bindings; every significant executor action produces a fresh observation and evidence reference before a pass can be accepted.

**Tech Stack:** Go 1.25, playwright-go, Gemini REST API, SQLite, React/TypeScript, Docker

**Spec:** `docs/superpowers/specs/2026-08-22-go-agent-parity-design.md`

## Global Constraints

- Existing `{url,instructions}` REST and MCP clients remain compatible and read-only by default.
- Secret values never enter SQLite, events, logs, reports, or model requests.
- Initial target origin is authorized; every additional HTTP(S) origin is explicit.
- Mutations require `allow_mutations=true`; destructive actions also require `allow_destructive=true`.
- Model tools use run-local element references, not arbitrary CSS selectors.
- Every significant action is followed by a fresh snapshot; every accepted pass has positive persisted evidence.
- SaaS-only organization, billing, scheduling, integration, and admin systems stay out of this repository.

---

### Task 1: Run authorization, origin policy, and durable non-secret metadata

**Files:**
- Create: `argus/internal/policy/policy.go`
- Create: `argus/internal/policy/policy_test.go`
- Modify: `argus/internal/domain/contract.go`
- Modify: `argus/internal/domain/contract_test.go`
- Modify: `argus/internal/store/store.go`
- Modify: `argus/internal/store/store_test.go`
- Modify: `argus/internal/server/server.go`
- Modify: `argus/internal/server/server_test.go`

**Interfaces:**
- Produces: `domain.RunAuthorization`, `domain.RunPolicy`, `policy.New`, `Policy.CheckNavigation`, `Policy.CheckAction`.
- Produces: `store.CreateRun(url, instructions string, policy domain.RunPolicy)` and backward-compatible schema migration.
- Consumes later: the browser adapter receives one immutable `*policy.Policy` per run.

- [ ] **Step 1: Write failing policy tests**

```go
func TestPolicyRestrictsOriginsAndMutations(t *testing.T) {
    p, err := policy.New("https://app.example.test/start", domain.RunAuthorization{
        AllowedOrigins: []string{"https://accounts.example.test"},
    })
    if err != nil { t.Fatal(err) }
    for _, allowed := range []string{"https://app.example.test/a", "https://accounts.example.test/oauth"} {
        if err := p.CheckNavigation(allowed); err != nil { t.Errorf("%s: %v", allowed, err) }
    }
    if err := p.CheckNavigation("http://169.254.169.254/latest/meta-data"); !errors.Is(err, policy.ErrOriginDenied) {
        t.Fatalf("navigation error = %v", err)
    }
    if err := p.CheckAction(policy.Action{Kind: policy.ActionSubmit}); !errors.Is(err, policy.ErrMutationDenied) {
        t.Fatalf("mutation error = %v", err)
    }
}

func TestPolicyRequiresExplicitAuthorityForDestructiveAction(t *testing.T) {
    p, _ := policy.New("https://app.example.test", domain.RunAuthorization{AllowMutations: true})
    if err := p.CheckAction(policy.Action{Kind: policy.ActionClick, Destructive: true}); !errors.Is(err, policy.ErrDestructiveDenied) {
        t.Fatalf("destructive error = %v", err)
    }
}
```

- [ ] **Step 2: Run the policy tests and verify RED**

Run: `cd argus && go test ./internal/policy`

Expected: FAIL because `internal/policy` and its public API do not exist.

- [ ] **Step 3: Implement the minimal policy API**

```go
type ActionKind string

const (
    ActionClick  ActionKind = "click"
    ActionType   ActionKind = "type"
    ActionSubmit ActionKind = "submit"
    ActionSelect ActionKind = "select"
)

type Action struct {
    Kind ActionKind
    Name string
    Destructive bool
}

type Policy struct {
    origins map[string]struct{}
    allowMutations bool
    allowDestructive bool
}

func (p *Policy) CheckNavigation(target string) error
func (p *Policy) CheckAction(action Action) error
```

Normalize origins as lowercase `scheme://host[:port]`, reject credentials/query/fragment in configured origins, and reject `allow_destructive=true` unless `allow_mutations=true`.

- [ ] **Step 4: Run the policy tests and verify GREEN**

Run: `cd argus && go test ./internal/policy`

Expected: PASS.

- [ ] **Step 5: Write failing domain/store/server compatibility tests**

```go
func TestCreateRequestAuthorizationDefaultsReadOnly(t *testing.T) {
    got, validation := decodeCreateRequest(strings.NewReader(`{"url":"https://example.com","instructions":"inspect"}`))
    if len(validation) != 0 || got.Authorization.AllowMutations || len(got.Authorization.AllowedOrigins) != 0 {
        t.Fatalf("request = %#v, validation = %#v", got, validation)
    }
}

func TestStoreMigratesAndPersistsOnlyRunPolicy(t *testing.T) {
    store := openLegacyStore(t)
    if err := store.Initialize(); err != nil { t.Fatal(err) }
    run, err := store.CreateRun("https://example.com", "test", domain.RunPolicy{
        AllowMutations: true,
        AllowedOrigins: []string{"https://accounts.example.com"},
    })
    if err != nil { t.Fatal(err) }
    if !run.Policy.AllowMutations || len(run.Policy.AllowedOrigins) != 1 { t.Fatalf("run = %#v", run) }
}
```

- [ ] **Step 6: Run focused tests and verify RED**

Run: `cd argus && go test ./internal/domain ./internal/store ./internal/server`

Expected: FAIL because authorization fields, migration columns, and the new `CreateRun` signature are absent.

- [ ] **Step 7: Add request/persisted types and migration**

```go
type RunAuthorization struct {
    AllowMutations bool              `json:"allow_mutations"`
    AllowDestructive bool            `json:"allow_destructive"`
    AllowedOrigins []string          `json:"allowed_origins"`
    SecretBindings map[string]string `json:"secret_bindings"`
}

type RunPolicy struct {
    AllowMutations bool     `json:"allow_mutations"`
    AllowedOrigins []string `json:"allowed_origins"`
}

type CreateRequest struct {
    URL string `json:"url"`
    Instructions string `json:"instructions"`
    Authorization RunAuthorization `json:"authorization"`
}
```

Add `allow_mutations INTEGER NOT NULL DEFAULT 0` and `allowed_origins_json TEXT NOT NULL DEFAULT '[]'` using idempotent `PRAGMA table_info` migration checks. Build `RunPolicy` from authorization without copying `SecretBindings`. Pass the complete ephemeral authorization from `createRun` into `Server.start` and ultimately `Runner.Run`.

- [ ] **Step 8: Run focused tests and commit**

Run: `cd argus && go test ./internal/domain ./internal/store ./internal/server`

Expected: PASS.

```bash
git add argus/internal/policy argus/internal/domain argus/internal/store argus/internal/server
git commit -m "feat: add run authorization policy"
```

### Task 2: Structured snapshots and stable semantic element references

**Files:**
- Modify: `argus/internal/browser/browser.go`
- Create: `argus/internal/browser/snapshot.go`
- Create: `argus/internal/browser/snapshot_test.go`
- Create: `argus/internal/browser/fixture_test.go`

**Interfaces:**
- Produces: `browser.PageSnapshot`, `browser.Element`, `browser.ActionResult`, `browser.WaitCondition`.
- Replaces `Session.Inspect() (Inspection,error)` with `Session.Inspect() (PageSnapshot,error)`.
- Replaces selector operations with element-reference operations and adds all required interaction methods.

- [ ] **Step 1: Write failing snapshot/reference tests**

```go
func TestSnapshotAssignsReferencesAndRejectsStaleOnes(t *testing.T) {
    session := openFixtureSession(t, `<label for="q">Search</label><input id="q" placeholder="Company"><button>Go</button>`)
    first, err := session.Inspect(context.Background())
    if err != nil { t.Fatal(err) }
    if first.Elements[0].Ref != "e1" || first.Elements[0].Label != "Search" { t.Fatalf("snapshot = %#v", first) }
    if _, err := session.Navigate(context.Background(), fixtureURL(t, `<button>Other</button>`)); err != nil { t.Fatal(err) }
    if _, err := session.Click(context.Background(), first.Elements[1].Ref); !errors.Is(err, browser.ErrStaleElement) {
        t.Fatalf("click error = %v", err)
    }
}
```

- [ ] **Step 2: Run the browser test and verify RED**

Run: `cd argus && go test ./internal/browser -run TestSnapshotAssignsReferencesAndRejectsStaleOnes`

Expected: FAIL because structured snapshots and reference-based clicks do not exist.

- [ ] **Step 3: Implement snapshot extraction and the reference map**

```go
type Element struct {
    Ref string `json:"ref"`
    Tag string `json:"tag"`
    Role string `json:"role,omitempty"`
    Name string `json:"name,omitempty"`
    Label string `json:"label,omitempty"`
    Placeholder string `json:"placeholder,omitempty"`
    InputType string `json:"input_type,omitempty"`
    Disabled bool `json:"disabled,omitempty"`
    Checked bool `json:"checked,omitempty"`
    Selected string `json:"selected,omitempty"`
}

type PageSnapshot struct {
    URL string `json:"url"`
    Title string `json:"title"`
    Text string `json:"text"`
    Width int `json:"width"`
    Height int `json:"height"`
    Elements []Element `json:"elements"`
}
```

Use one page-evaluated JavaScript extraction pass over anchors, buttons, inputs, selects, textareas, and explicit ARIA roles. Store Playwright locators keyed by generation plus `eN`; increment the generation after navigation and every fresh inspection.

- [ ] **Step 4: Run the reference test and verify GREEN**

Run: `cd argus && go test ./internal/browser -run TestSnapshotAssignsReferencesAndRejectsStaleOnes`

Expected: PASS.

- [ ] **Step 5: Write failing fixture tests for the complete browser surface**

```go
func TestPlaywrightSemanticInteractionSurface(t *testing.T) {
    session := openFixtureApp(t)
    page := inspect(t, session)
    mustType(t, session, refByLabel(page, "Search"), "Airbnb")
    mustSelect(t, session, refByLabel(page, "Batch"), "Winter 2024")
    mustPress(t, session, "Enter")
    mustWait(t, session, browser.WaitCondition{Text: "Airbnb", Timeout: time.Second})
    mustScroll(t, session, 600)
    mustResize(t, session, 375, 812)
    after := inspect(t, session)
    if after.Width != 375 || !strings.Contains(after.Text, "Airbnb") { t.Fatalf("after = %#v", after) }
}
```

- [ ] **Step 6: Run the fixture test and verify RED**

Run: `cd argus && ARGUS_PLAYWRIGHT_SMOKE=1 go test ./internal/browser -run TestPlaywrightSemanticInteractionSurface`

Expected: FAIL on the first missing interaction method.

- [ ] **Step 7: Implement the complete session interface**

```go
type Session interface {
    Navigate(context.Context, string) (Navigation, error)
    Inspect(context.Context) (PageSnapshot, error)
    Click(context.Context, string) (ActionResult, error)
    Type(context.Context, string, string) (ActionResult, error)
    FillForm(context.Context, map[string]string) (ActionResult, error)
    Submit(context.Context, string) (ActionResult, error)
    Select(context.Context, string, string) (ActionResult, error)
    Press(context.Context, string) (ActionResult, error)
    Scroll(context.Context, int) (ActionResult, error)
    Resize(context.Context, int, int) (ActionResult, error)
    Wait(context.Context, WaitCondition) (ActionResult, error)
    ConsoleErrors(context.Context) ([]string, error)
    NetworkErrors(context.Context) ([]NetworkError, error)
    ClickPoint(context.Context, int, int) (ActionResult, error)
    Screenshot(context.Context, string) error
    Close() error
}
```

Attach console/response listeners when creating the page. Bound console and network buffers to 100 entries and strip headers, bodies, and sensitive query parameters.

- [ ] **Step 8: Run browser tests and commit**

Run: `cd argus && ARGUS_PLAYWRIGHT_SMOKE=1 go test ./internal/browser`

Expected: PASS.

```bash
git add argus/internal/browser
git commit -m "feat: add semantic browser interaction surface"
```

### Task 3: Ephemeral secrets, redaction, and tool-level action enforcement

**Files:**
- Create: `argus/internal/runner/secrets.go`
- Create: `argus/internal/runner/secrets_test.go`
- Modify: `argus/internal/runner/pipeline.go`
- Modify: `argus/internal/runner/prompts.go`
- Modify: `argus/internal/runner/runner.go`
- Modify: `argus/internal/runner/runner_test.go`
- Modify: `argus/internal/browser/events.go`
- Modify: `argus/internal/browser/events_test.go`

**Interfaces:**
- Produces: `secretSet.Resolve(name)`, `secretSet.Redact(text)`, `browserAdapter` methods for every browser tool.
- Consumes: `policy.Policy` and the reference-based `browser.Session` from Tasks 1–2.

- [ ] **Step 1: Write failing secret and redaction tests**

```go
func TestSecretsResolveWithoutEnteringEventsOrReports(t *testing.T) {
    secrets, err := newSecretSet(map[string]string{"login_password": "correct horse battery staple"})
    if err != nil { t.Fatal(err) }
    if value, ok := secrets.Resolve("login_password"); !ok || value != "correct horse battery staple" { t.Fatal("not resolved") }
    got := secrets.Redact("failed with correct horse battery staple")
    if got != "failed with [REDACTED]" { t.Fatalf("redacted = %q", got) }
}
```

- [ ] **Step 2: Run and verify RED**

Run: `cd argus && go test ./internal/runner -run TestSecretsResolveWithoutEnteringEventsOrReports`

Expected: FAIL because `secretSet` does not exist.

- [ ] **Step 3: Implement immutable run-scoped secrets**

Reject empty names/values, cap bindings at 20, cap each value at 4 KiB, sort values longest-first for redaction, and zero the copied byte slices when the run exits.

- [ ] **Step 4: Run and verify GREEN**

Run: `cd argus && go test ./internal/runner -run TestSecretsResolveWithoutEnteringEventsOrReports`

Expected: PASS.

- [ ] **Step 5: Write failing adapter policy tests**

```go
func TestTypeTextResolvesBindingAndPersistsOnlyRedactedMetadata(t *testing.T) {
    adapter := authorizedAdapter(t, map[string]string{"login_password": "secret-value"})
    result, err := adapter.typeText(context.Background(), map[string]any{"ref":"e1", "secret":"login_password"}, agent.ToolContext{})
    if err != nil { t.Fatal(err) }
    if adapter.session.typed != "secret-value" { t.Fatalf("typed = %q", adapter.session.typed) }
    encoded, _ := json.Marshal(result)
    if bytes.Contains(encoded, []byte("secret-value")) { t.Fatalf("result leaked: %s", encoded) }
}

func TestCoordinateClickCannotBypassMutationPolicy(t *testing.T) {
    adapter := readOnlyAdapter(t)
    if _, err := adapter.visualClick(context.Background(), map[string]any{"description":"Delete account"}, agent.ToolContext{}); !errors.Is(err, policy.ErrMutationDenied) {
        t.Fatalf("error = %v", err)
    }
}
```

- [ ] **Step 6: Run adapter tests and verify RED**

Run: `cd argus && go test ./internal/runner -run 'Test(TypeText|CoordinateClick)'`

Expected: FAIL because the new tools and enforced policy path are absent.

- [ ] **Step 7: Implement the agent tool adapters**

Expose exact schemas using `ref` for semantic elements. `type_text` accepts exactly one of `text` or `secret`; `fill_form` accepts a bounded array of `{ref,text}` or `{ref,secret}`. Apply policy before calling the browser. After each significant action, call `Inspect` and return `{action,url,snapshot}`. Persist action metadata via `browser.FormatAction`, excluding text, secret names, page text, and network bodies.

- [ ] **Step 8: Run runner/event tests and commit**

Run: `cd argus && go test ./internal/runner ./internal/browser`

Expected: PASS.

```bash
git add argus/internal/runner argus/internal/browser/events.go argus/internal/browser/events_test.go
git commit -m "feat: enforce safe browser tools and secret redaction"
```

### Task 4: Multimodal tool-result feedback

**Files:**
- Modify: `argus/internal/agent/types.go`
- Modify: `argus/internal/agent/runtime.go`
- Modify: `argus/internal/agent/runtime_test.go`
- Modify: `argus/internal/gemini/provider.go`
- Modify: `argus/internal/gemini/provider_test.go`
- Modify: `argus/internal/runner/pipeline.go`
- Modify: `argus/internal/runner/runner_test.go`

**Interfaces:**
- Produces: `agent.ToolOutput{Result any, Followup []MessagePart}`.
- Runtime appends a normal function response followed by a user multimodal observation before the next provider call.

- [ ] **Step 1: Write a failing runtime ordering test**

```go
func TestRuntimeFeedsToolScreenshotIntoNextModelTurn(t *testing.T) {
    tool := Tool{Name:"screenshot", Invoke: func(context.Context, map[string]any, ToolContext) (any,error) {
        return ToolOutput{Result: map[string]any{"path":"/shot.png"}, Followup: []MessagePart{{Image:&ImagePart{Data:[]byte("png"), MediaType:"image/png"}}}}, nil
    }}
    provider := providerWithResponses(toolCall("screenshot"), textResponse("done"))
    runtime := NewRuntime(map[string]Provider{"test":provider}, NewInMemorySessionStore())
    if err := runtime.Run(context.Background(), specWith(tool), "s", user("go"), nil, nil); err != nil { t.Fatal(err) }
    second := provider.Requests[1].Messages
    if second[len(second)-1].Parts[0].Image == nil { t.Fatalf("messages = %#v", second) }
}
```

- [ ] **Step 2: Run and verify RED**

Run: `cd argus && go test ./internal/agent -run TestRuntimeFeedsToolScreenshotIntoNextModelTurn`

Expected: FAIL because `ToolOutput` and follow-up parts do not exist.

- [ ] **Step 3: Implement multimodal tool output**

```go
type ToolOutput struct {
    Result any
    Followup []MessagePart
}
```

When a tool returns `ToolOutput`, append the `RoleTool` function result first, then append a `RoleUser` message containing only validated text/image/audio follow-up parts. Reject tool calls or tool results inside `Followup`.

- [ ] **Step 4: Run agent and Gemini provider tests**

Run: `cd argus && go test ./internal/agent ./internal/gemini`

Expected: PASS, including exact Gemini JSON ordering.

- [ ] **Step 5: Make screenshot return the captured PNG and test non-persistence**

Change `browserAdapter.screenshot` to return `agent.ToolOutput` with the bounded path/label JSON and one PNG `ImagePart`. Extend the runner test to assert the next request contains image bytes while encoded events and SQLite do not.

- [ ] **Step 6: Run runner tests and commit**

Run: `cd argus && go test ./internal/runner`

Expected: PASS.

```bash
git add argus/internal/agent argus/internal/gemini argus/internal/runner
git commit -m "feat: return live screenshots to agents"
```

### Task 5: Visual grounding and guarded coordinate clicks

**Files:**
- Create: `argus/internal/runner/grounding.go`
- Create: `argus/internal/runner/grounding_test.go`
- Modify: `argus/internal/runner/runner.go`
- Modify: `argus/internal/runner/pipeline.go`
- Modify: `argus/internal/runner/prompts.go`
- Modify: `argus/internal/gemini/provider.go`
- Modify: `argus/internal/gemini/provider_test.go`

**Interfaces:**
- Produces: `runner.Grounder.Locate(ctx, request) ([]VisualMatch,error)`.
- Produces: `gemini.Provider.Locate` using the configured model and JSON response mode.
- Consumes: current screenshot bytes, viewport dimensions, and policy engine.

- [ ] **Step 1: Write failing coordinate and confidence tests**

```go
func TestGroundingRejectsInvalidAndLowConfidenceMatches(t *testing.T) {
    for _, match := range []VisualMatch{{X:-1,Y:10,Confidence:.9}, {X:20,Y:20,Confidence:.4}, {X:900,Y:20,Confidence:.9}} {
        if _, err := validateMatch(match, 800, 600, .70); err == nil { t.Fatalf("accepted %#v", match) }
    }
    got, err := validateMatch(VisualMatch{X:20,Y:30,Confidence:.91,Description:"Search"}, 800, 600, .70)
    if err != nil || got.X != 20 { t.Fatalf("got = %#v, %v", got, err) }
}
```

- [ ] **Step 2: Run and verify RED**

Run: `cd argus && go test ./internal/runner -run TestGroundingRejectsInvalidAndLowConfidenceMatches`

Expected: FAIL because grounding types and validation do not exist.

- [ ] **Step 3: Implement grounding types and Gemini request**

```go
type GroundingRequest struct {
    Description string
    Image []byte
    Width int
    Height int
    Limit int
}

type VisualMatch struct {
    X int `json:"x"`
    Y int `json:"y"`
    Confidence float64 `json:"confidence"`
    Description string `json:"description"`
}
```

Use a dedicated one-shot multimodal Gemini request with JSON mode. Cap descriptions at 500 runes, matches at 10, image size at 10 MiB, and visual calls at 12 per pipeline stage.

- [ ] **Step 4: Run grounding/provider tests and verify GREEN**

Run: `cd argus && go test ./internal/runner ./internal/gemini`

Expected: PASS.

- [ ] **Step 5: Write failing `find_elements` and `visual_click` adapter tests**

Assert that `find_elements` returns validated matches plus an image follow-up, `visual_click` takes a fresh screenshot, policy checks the grounded description before clicking, and low-confidence matches never call `ClickPoint`.

- [ ] **Step 6: Implement the two tools and run tests**

Run: `cd argus && go test ./internal/runner`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add argus/internal/runner argus/internal/gemini
git commit -m "feat: add guarded visual grounding"
```

### Task 6: Strict stage contracts and evidence-gated verdicts

**Files:**
- Create: `argus/internal/runner/contracts.go`
- Create: `argus/internal/runner/contracts_test.go`
- Modify: `argus/internal/runner/prompts.go`
- Modify: `argus/internal/runner/runner.go`
- Modify: `argus/internal/runner/pipeline.go`
- Modify: `argus/internal/runner/runner_test.go`

**Interfaces:**
- Produces: validated `TestBrief`, `AppMap`, `TestPlan`, `ExecutionResult`, and `CriticResult` structs.
- Produces: `normalizeVerdict(critic, execution, evidence) domain.RunReport`.

- [ ] **Step 1: Write failing evidence-floor tests**

```go
func TestPassWithoutEvidenceBecomesInconclusive(t *testing.T) {
    report := normalizeVerdict(
        CriticResult{Verdict:"passed", Summary:"looks good"},
        ExecutionResult{Cases: []CaseResult{{ID:"T1", Status:"passed"}}},
        EvidenceIndex{},
    )
    if report.Verdict != domain.ReportVerdictInconclusive { t.Fatalf("report = %#v", report) }
}

func TestEveryRequestedCaseNeedsPositiveEvidence(t *testing.T) {
    evidence := EvidenceIndex{"shot-1": {Kind:"screenshot", Path:"/screenshots/run/shot-1.png"}}
    execution := ExecutionResult{Cases: []CaseResult{{ID:"T1",Status:"passed",Evidence:[]string{"shot-1"}}, {ID:"T2",Status:"passed"}}}
    if got := evidenceVerdict(execution, evidence); got != domain.ReportVerdictInconclusive { t.Fatalf("got = %s", got) }
}
```

- [ ] **Step 2: Run and verify RED**

Run: `cd argus && go test ./internal/runner -run 'Test(PassWithoutEvidence|EveryRequestedCase)'`

Expected: FAIL because typed contracts and evidence normalization do not exist.

- [ ] **Step 3: Implement strict contracts and one repair attempt**

Decode fenced/unfenced JSON into typed structs, reject unknown verdicts, empty case IDs, duplicate IDs, missing requested cases, oversized lists, and evidence references not present in the run's evidence index. On first decode failure, call the same stage once with the validation error and original output; a second failure returns an inconclusive stage error.

- [ ] **Step 4: Update prompts with exact schemas and action-observation invariant**

Executor output must contain `cases[].id`, `status`, `steps`, `findings`, and `evidence`. Critic output remains report-shaped but cannot override the deterministic evidence floor.

- [ ] **Step 5: Run contract tests and verify GREEN**

Run: `cd argus && go test ./internal/runner`

Expected: PASS.

- [ ] **Step 6: Update the deterministic full-pipeline test**

Script a search run that inspects, types, waits, captures a screenshot, cites its path, and passes. Add the inverse case where Gemini claims pass without evidence and assert stored status/report are inconclusive/failed.

- [ ] **Step 7: Run and commit**

Run: `cd argus && go test ./internal/runner ./internal/server`

Expected: PASS.

```bash
git add argus/internal/runner
git commit -m "feat: require proof for agent verdicts"
```

### Task 7: Real Playwright fixture application and end-to-end agent-tool integration

**Files:**
- Create: `argus/internal/browser/testdata/fixture.html`
- Create: `argus/internal/browser/integration_test.go`
- Create: `argus/internal/runner/integration_test.go`
- Modify: `argus/internal/browser/playwright_smoke_test.go`

**Interfaces:**
- Produces: one local fixture that exercises search, form validation/submission, select, delayed content, responsive navigation, console failure, safe mutation, destructive control, and cross-origin redirect.

- [ ] **Step 1: Add the fixture and failing integration assertions**

The test server serves `fixture.html`, `/api/save`, `/delayed`, and `/redirect`. The browser test performs each public tool through real Playwright and asserts visible state changes, viewport changes, console/network capture, redacted screenshots, and redirect denial.

- [ ] **Step 2: Run integration tests and verify RED**

Run: `cd argus && ARGUS_PLAYWRIGHT_SMOKE=1 go test ./internal/browser ./internal/runner -run Integration`

Expected: FAIL at any browser/tool behavior not yet correctly wired.

- [ ] **Step 3: Fix only integration defects exposed by the fixture**

Keep fixes inside existing browser, policy, and runner boundaries. Add a focused regression test before each defect fix, then rerun that focused test to RED and GREEN.

- [ ] **Step 4: Run the complete Go suite with Playwright**

Run: `cd argus && ARGUS_PLAYWRIGHT_SMOKE=1 go test -race ./...`

Expected: PASS with no race reports.

- [ ] **Step 5: Commit**

```bash
git add argus/internal/browser argus/internal/runner
git commit -m "test: cover Go agent against a real browser fixture"
```

### Task 8: Clean-clone packaging, public documentation, and release verification

**Files:**
- Create: `.dockerignore`
- Modify: `.gitignore`
- Modify: `argus/cmd/argus/main_test.go`
- Modify: `argus/cmd/argus/main.go`
- Modify: `README.md`
- Modify: `frontend/src/components/dashboard/NewTestInput.tsx`
- Modify: `frontend/src/types.ts`

**Interfaces:**
- Produces: clean-checkout tests independent of `frontend/dist`/`argus/static`.
- Produces: UI controls for read-only versus mutation-authorized runs and additional allowed origins; secrets remain API/environment-only until a dedicated secure UI is designed.

- [ ] **Step 1: Write a failing static-directory unit test independent of build output**

```go
func TestFindStaticDirWalksFromProvidedDirectory(t *testing.T) {
    root := t.TempDir()
    want := filepath.Join(root, "argus", "static")
    if err := os.MkdirAll(want, 0o755); err != nil { t.Fatal(err) }
    if err := os.WriteFile(filepath.Join(want, "index.html"), []byte("ok"), 0o644); err != nil { t.Fatal(err) }
    nested := filepath.Join(root, "argus", "cmd", "argus")
    if err := os.MkdirAll(nested, 0o755); err != nil { t.Fatal(err) }
    if got := findStaticDir(nested); got != want { t.Fatalf("got %q, want %q", got, want) }
}
```

- [ ] **Step 2: Run and verify RED, then extract `findStaticDir` and verify GREEN**

Run: `cd argus && go test ./cmd/argus`

Expected before implementation: FAIL because `findStaticDir` does not exist. Expected after extraction: PASS without a frontend build.

- [ ] **Step 3: Add release ignore files**

`.dockerignore` must include `.git`, `.env*` with `!.env.example`, `data`, `frontend/node_modules`, `frontend/dist`, `argus/static`, Python virtual environments, `__pycache__`, `*.pyc`, and test caches. Add the Python patterns to `.gitignore` as well.

- [ ] **Step 4: Update the composer and types**

Add an advanced policy section with `allow_mutations` and newline-separated `allowed_origins`; submit it as `authorization`. Do not add raw-secret inputs to the browser UI. Type the request and displayed run policy in `frontend/src/types.ts`.

- [ ] **Step 5: Rewrite README capability and safety sections**

Document six stages, the exact tool surface, read-only default, mutation authorization, origin allowlist, environment/API secret bindings, screenshot grounding, offline versus Gemini smoke tests, source-available licensing, and the correct build-before-run commands.

- [ ] **Step 6: Run fresh complete verification**

```bash
cd argus && go test -race ./... && go vet ./...
cd ../frontend && npm ci && npm test && npm run typecheck && npm run lint && npm run build
cd .. && docker build -t argus-agent-parity:local .
```

Expected: every command exits 0.

- [ ] **Step 7: Run production-container smoke verification**

Start the image on an unused loopback port with a temporary data volume. Verify `/`, `/api/settings`, creating a read-only deterministic fixture run, WebSocket event completion, screenshot retrieval, and cancellation. Stop and remove the named test container afterward.

- [ ] **Step 8: Audit leakage and repository state**

Run tracked-file scans for Python bytecode, private Python source, common credential signatures, and ignored secret/data files. Confirm only intentional task changes are staged and all pre-existing untracked artifacts remain untouched.

- [ ] **Step 9: Commit**

```bash
git add .dockerignore .gitignore README.md argus/cmd/argus frontend/src
git commit -m "docs: ship reproducible standalone Go agent"
```
