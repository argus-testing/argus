package runner

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ace-foundry/argus-testing/argus/internal/agent"
	"github.com/ace-foundry/argus-testing/argus/internal/browser"
	"github.com/ace-foundry/argus-testing/argus/internal/domain"
	"github.com/ace-foundry/argus-testing/argus/internal/server"
	"github.com/ace-foundry/argus-testing/argus/internal/store"
)

type fakeFactory struct {
	session browser.Session
	open    func(context.Context) error
}

func (f fakeFactory) Open(ctx context.Context) (browser.Session, error) {
	if f.open != nil {
		if err := f.open(ctx); err != nil {
			return nil, err
		}
	}
	return f.session, nil
}

type fakeSession struct {
	browser.Session
	navigateErr     error
	screenshotErr   error
	closed          int
	screenshotPath  string
	screenshotData  [][]byte
	screenshotCount int
	calls           []string
}

func (s *fakeSession) Navigate(_ context.Context, url string) (browser.Navigation, error) {
	s.calls = append(s.calls, "navigate")
	return browser.Navigation{URL: url, Title: "Example"}, s.navigateErr
}
func (s *fakeSession) Inspect(context.Context) (browser.PageSnapshot, error) {
	s.calls = append(s.calls, "inspect")
	return browser.PageSnapshot{URL: "https://example.com", Title: "Secret title", Text: "private page text", Elements: []browser.Element{{Ref: "e1-1", Name: "private button"}}}, nil
}
func (s *fakeSession) Click(context.Context, string) (browser.ActionResult, error) {
	s.calls = append(s.calls, "click")
	return browser.ActionResult{URL: "https://example.com"}, nil
}
func (s *fakeSession) Type(context.Context, string, string) (browser.ActionResult, error) {
	s.calls = append(s.calls, "type")
	return browser.ActionResult{URL: "https://example.com"}, nil
}
func (s *fakeSession) Screenshot(_ context.Context, path string) error {
	s.calls = append(s.calls, "screenshot")
	s.screenshotPath = path
	if s.screenshotErr != nil {
		return s.screenshotErr
	}
	data := []byte("png")
	if s.screenshotCount < len(s.screenshotData) {
		data = s.screenshotData[s.screenshotCount]
	}
	s.screenshotCount++
	return os.WriteFile(path, data, 0o644)
}
func (s *fakeSession) Close() error { s.closed++; return nil }

type scriptedProvider struct {
	mu        sync.Mutex
	responses []agent.ModelResponse
	names     []string
	requests  []agent.ModelRequest
}

func (p *scriptedProvider) Stream(_ context.Context, request agent.ModelRequest, emit func(agent.ModelEvent) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.names = append(p.names, request.Model.Model+":"+request.SystemInstruction[:min(12, len(request.SystemInstruction))])
	p.requests = append(p.requests, request)
	if len(p.responses) == 0 {
		return errors.New("unexpected model call")
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return emit(response)
}
func response(text string) agent.ModelResponse {
	return agent.ModelResponse{Parts: []agent.MessagePart{{Text: &agent.TextPart{Text: text}}}}
}
func tool(name string, values map[string]any) agent.ModelResponse {
	return agent.ModelResponse{Parts: []agent.MessagePart{{ToolCall: &agent.ToolCallPart{CallID: name, Name: name, Arguments: values}}}}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestNewUsesDefaultModelWhenUnset(t *testing.T) {
	if model := New(nil, nil, Options{}).model.Model; model != "gemini-2.5-flash" {
		t.Fatalf("model = %q", model)
	}
}

func newTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "argus.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Initialize(); err != nil {
		t.Fatal(err)
	}
	return db, path
}
func queuedRun(t *testing.T, db *store.Store) *domain.Run {
	t.Helper()
	run, err := db.CreateRun("https://example.com", "check search", domain.RunPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddEvent(run.ID, domain.EventRunQueued, nil); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestRunnerRunsPublicPipelineAndPersistsSafeBrowserEvents(t *testing.T) {
	db, databasePath := newTestStore(t)
	run := queuedRun(t, db)
	session := &fakeSession{screenshotData: [][]byte{[]byte("initial-shot"), []byte("pre-execution-shot"), []byte("final-shot")}}
	provider := &scriptedProvider{responses: []agent.ModelResponse{
		response(`{"testable":true}`), response(`{"intent":"search"}`),
		tool("inspect_page", map[string]any{}), response(`{"pages":[]}`), response(`{"tests":[]}`),
		tool("click", map[string]any{"ref": "e1-1"}), response(`{"status":"passed"}`),
		response("```json\n{\"verdict\":\"passed\",\"summary\":\"verified\",\"findings\":[{\"severity\":\"info\",\"title\":\"ok\",\"detail\":\"works\"}],\"recommendations\":[\"keep it\"]}\n```"),
	}}
	r := New(db, fakeFactory{session: session}, Options{ScreenshotDir: filepath.Join(t.TempDir(), "screenshots"), Provider: provider})
	r.Run(context.Background(), run.ID, domain.RunAuthorization{})
	current, err := db.GetRun(run.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != domain.RunStatusPassed || current.Report == nil || current.Report.Verdict != domain.ReportVerdictPassed {
		t.Fatalf("run = %#v", current)
	}
	if session.closed != 1 {
		t.Fatalf("close calls = %d", session.closed)
	}
	if len(provider.names) != 8 {
		t.Fatalf("model calls = %d", len(provider.names))
	}
	var screenshots, actions, observations int
	for _, event := range current.Events {
		switch event.Type {
		case domain.EventBrowserScreenshot:
			screenshots++
		case domain.EventBrowserAction:
			actions++
			if event.Data["tool"] == "type_text" {
				t.Fatalf("unexpected form-fill action = %#v", event)
			}
		case domain.EventBrowserObservation:
			observations++
			if event.Data["tool"] == "inspect_page" {
				result := event.Data["result"].(map[string]any)
				if _, ok := result["text"]; ok || result["summary"] != "Page inspection omitted from persisted events" {
					t.Fatalf("observation leaked: %#v", event)
				}
			}
		}
	}
	if screenshots != 2 || actions != 2 || observations != 2 {
		t.Fatalf("events screenshots/actions/observations = %d/%d/%d", screenshots, actions, observations)
	}
	for _, test := range []struct {
		index int
		image string
	}{{2, "initial-shot"}, {5, "pre-execution-shot"}, {7, "final-shot"}} {
		parts := provider.requests[test.index].Messages[0].Parts
		if len(parts) != 2 || parts[1].Image == nil || parts[1].Image.MediaType != "image/png" || string(parts[1].Image.Data) != test.image {
			t.Fatalf("request %d image = %#v", test.index, parts)
		}
	}
	for _, event := range current.Events {
		encoded, _ := json.Marshal(event)
		for _, value := range []string{"private page text", "private button", base64.StdEncoding.EncodeToString([]byte("initial-shot")), base64.StdEncoding.EncodeToString([]byte("pre-execution-shot")), base64.StdEncoding.EncodeToString([]byte("final-shot"))} {
			if strings.Contains(string(encoded), value) {
				t.Fatalf("event persisted private data: %s", encoded)
			}
		}
	}
	reader, err := sql.Open("sqlite", "file:"+databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var count int
	if err := reader.QueryRow("SELECT COUNT(*) FROM screenshots WHERE run_id = ?", run.ID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("screenshots = %d, %v", count, err)
	}
}

func TestRunnerRejectsUnadvertisedTypeTextWithoutFill(t *testing.T) {
	db, _ := newTestStore(t)
	run := queuedRun(t, db)
	session := &fakeSession{}
	provider := &scriptedProvider{responses: []agent.ModelResponse{
		response(`{"testable":true}`), response(`{"intent":"search"}`),
		tool("type_text", map[string]any{"selector": "#input", "text": "credential"}),
	}}
	r := New(db, fakeFactory{session: session}, Options{ScreenshotDir: t.TempDir(), Provider: provider})
	r.Run(context.Background(), run.ID, domain.RunAuthorization{})

	current, err := db.GetRun(run.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != domain.RunStatusFailed {
		t.Fatalf("run status = %s", current.Status)
	}
	for _, call := range session.calls {
		if call == "type" {
			t.Fatal("Type was invoked for an unadvertised type_text call")
		}
	}
	if len(provider.requests) != 3 {
		t.Fatalf("model requests = %d", len(provider.requests))
	}
	for _, browserTool := range provider.requests[2].Tools {
		if browserTool.Name == "type_text" {
			t.Fatal("type_text was advertised to the model")
		}
	}
	for _, event := range current.Events {
		if event.Type == domain.EventBrowserAction && event.Data["tool"] == "type_text" {
			t.Fatalf("type_text event persisted: %#v", event)
		}
	}
}

func TestBrowserAdapterReturnsClickAndNavigateObservations(t *testing.T) {
	adapter := &browserAdapter{session: &fakeSession{}}
	ctx := context.Background()

	navigate, err := adapter.navigate(ctx, map[string]any{"url": "https://example.com/a?view=all"}, agent.ToolContext{})
	if err != nil || !reflect.DeepEqual(navigate, map[string]any{"url": "https://example.com/a?view=all", "title": "Example"}) {
		t.Fatalf("navigate = %#v, %v", navigate, err)
	}
	click, err := adapter.click(ctx, map[string]any{"ref": "e1-1"}, agent.ToolContext{})
	if err != nil || !reflect.DeepEqual(click, map[string]any{"url": "https://example.com", "result": "clicked"}) {
		t.Fatalf("click = %#v, %v", click, err)
	}
}

func TestReportNormalizationAndMissingKeyFailure(t *testing.T) {
	report := parseReport("not json", "plan", "execution")
	if report.Verdict != domain.ReportVerdictInconclusive || report.Summary != "not json" || report.Plan == nil {
		t.Fatalf("report = %#v", report)
	}
	db, _ := newTestStore(t)
	run := queuedRun(t, db)
	t.Setenv("GEMINI_API_KEY", "")
	New(db, fakeFactory{}, Options{ScreenshotDir: t.TempDir()}).Run(context.Background(), run.ID, domain.RunAuthorization{})
	current, err := db.GetRun(run.ID, false)
	if err != nil || current.Status != domain.RunStatusFailed || current.Error == nil || *current.Error != missingAPIKey {
		t.Fatalf("run = %#v, %v", current, err)
	}
}

func TestRunnerDoesNotOverwriteCancellationRace(t *testing.T) {
	db, _ := newTestStore(t)
	run := queuedRun(t, db)
	opened := make(chan struct{})
	var once sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := &scriptedProvider{responses: []agent.ModelResponse{response(`{"testable":true}`), response(`{}`)}}
	r := New(db, fakeFactory{open: func(ctx context.Context) error { once.Do(func() { close(opened) }); <-ctx.Done(); return ctx.Err() }}, Options{ScreenshotDir: t.TempDir(), Provider: provider})
	done := make(chan struct{})
	go func() { r.Run(ctx, run.ID, domain.RunAuthorization{}); close(done) }()
	<-opened
	cancelled, err := db.Transition(run.ID, []domain.RunStatus{domain.RunStatusRunning}, domain.RunStatusCancelled, domain.EventRunCancelled, nil, nil, nil)
	if err != nil || cancelled == nil {
		t.Fatalf("cancel transition = %#v, %v", cancelled, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop")
	}
	current, err := db.GetRun(run.ID, true)
	if err != nil || current.Status != domain.RunStatusCancelled {
		t.Fatalf("run = %#v, %v", current, err)
	}
}

func TestNextScreenshotPathRejectsUnsafeRunIDs(t *testing.T) {
	for _, runID := range []string{".", "..", "a/b", "0123456789ABCDEF0123456789ABCDEF"} {
		if _, _, err := NextScreenshotPath(t.TempDir(), runID, "initial"); err == nil {
			t.Errorf("NextScreenshotPath(%q) error = nil", runID)
		}
	}
}
func TestNextScreenshotPathUsesSafeLabelAndSequence(t *testing.T) {
	root := t.TempDir()
	runID := "0123456789abcdef0123456789abcdef"
	public, disk, err := NextScreenshotPath(root, runID, " Initial / Page ")
	if err != nil {
		t.Fatal(err)
	}
	if public != "/screenshots/"+runID+"/initial---page-1.png" {
		t.Fatalf("public path = %q", public)
	}
	if err := os.WriteFile(disk, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	public, _, err = NextScreenshotPath(root, runID, "evidence")
	if err != nil {
		t.Fatal(err)
	}
	if public != "/screenshots/"+runID+"/evidence-2.png" {
		t.Fatalf("second public path = %q", public)
	}
}

func TestServerPostReachesTerminalReportWithFakePipeline(t *testing.T) {
	db, _ := newTestStore(t)
	provider := &scriptedProvider{responses: []agent.ModelResponse{
		response(`{"testable":true}`), response(`{"intent":"search"}`), response(`{"pages":[]}`), response(`{"tests":[]}`), response(`{"status":"passed"}`), response(`{"verdict":"passed","summary":"done"}`),
	}}
	r := New(db, fakeFactory{session: &fakeSession{}}, Options{ScreenshotDir: t.TempDir(), Provider: provider})
	handler, err := server.New(db, r, server.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r.SetPublisher(handler.Publish)
	request := httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{"url":"https://example.com","instructions":"test search"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d", response.Code)
	}
	var created domain.Run
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, err := db.GetRun(created.ID, false)
		if err == nil && current != nil && current.Status == domain.RunStatusPassed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server run did not reach passed terminal report")
}

func TestReportParserAcceptsFences(t *testing.T) {
	value := parseReport("```json\n{\"verdict\":\"failed\",\"summary\":\"x\"}\n```", "p", "o")
	encoded, _ := json.Marshal(value)
	if value.Verdict != domain.ReportVerdictFailed || len(encoded) == 0 {
		t.Fatalf("report = %#v", value)
	}
}

func TestReportParserFiltersMalformedEntries(t *testing.T) {
	value := parseReport(`{"verdict":"failed","summary":"verified","findings":[{"severity":"high","title":"broken","detail":"details"},null,{"severity":"low","title":3,"detail":"invalid"}],"recommendations":["retry",7,null]}`, "plan", "observations")
	if value.Verdict != domain.ReportVerdictFailed || value.Summary != "verified" || value.Plan == nil || *value.Plan != "plan" || len(value.Findings) != 1 || value.Findings[0].Title != "broken" || len(value.Recommendations) != 1 || value.Recommendations[0] != "retry" {
		t.Fatalf("report = %#v", value)
	}
}

func TestRunnerPersistsTimeoutKind(t *testing.T) {
	db, _ := newTestStore(t)
	run := queuedRun(t, db)
	provider := &scriptedProvider{responses: []agent.ModelResponse{response(`{"testable":true}`), response(`{}`)}}
	r := New(db, fakeFactory{open: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}, Options{ScreenshotDir: t.TempDir(), Timeout: 10 * time.Millisecond, Provider: provider})
	r.Run(context.Background(), run.ID, domain.RunAuthorization{})
	current, err := db.GetRun(run.ID, true)
	if err != nil || current.Status != domain.RunStatusFailed {
		t.Fatalf("run = %#v, %v", current, err)
	}
	for _, event := range current.Events {
		if event.Type == domain.EventRunFailed && event.Data["kind"] == "timeout" {
			return
		}
	}
	t.Fatalf("timeout failure event = %#v", current.Events)
}
