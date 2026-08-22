package runner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ace-foundry/argus-testing/argus/internal/agent"
	"github.com/ace-foundry/argus-testing/argus/internal/browser"
	"github.com/ace-foundry/argus-testing/argus/internal/domain"
)

func TestIntegrationFullRunnerUsesRealPlaywrightAndEvidence(t *testing.T) {
	if os.Getenv("ARGUS_PLAYWRIGHT_SMOKE") != "1" {
		t.Skip("set ARGUS_PLAYWRIGHT_SMOKE=1 after running argus install-browser")
	}
	fixture, err := os.ReadFile(filepath.Join("..", "browser", "testdata", "fixture.html"))
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/":
			_, _ = w.Write(fixture)
		case "/api/save":
			_, _ = w.Write([]byte("Preference saved"))
		case "/missing":
			http.Error(w, "missing", http.StatusNotFound)
		default:
			http.NotFound(w, request)
		}
	}))
	defer app.Close()

	db, _ := newTestStore(t)
	run, err := db.CreateRun(app.URL, "Search for Airbnb and verify the visible result", domain.RunPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddEvent(run.ID, domain.EventRunQueued, nil); err != nil {
		t.Fatal(err)
	}
	evidencePath := fmt.Sprintf("/screenshots/%s/integration-result-3.png", run.ID)
	provider := &scriptedProvider{responses: []agent.ModelResponse{
		response("{\"testable\":true}"),
		response("{\"objective\":\"Verify company search\",\"features\":[\"Company search\"],\"constraints\":[\"Read-only\"]}"),
		tool("inspect_page", map[string]any{}),
		response(fmt.Sprintf("{\"pages\":[{\"url\":%q,\"name\":\"QA fixture\",\"features\":[\"Company search\"]}]}", app.URL)),
		response("{\"cases\":[{\"id\":\"T1\",\"name\":\"Company search\",\"steps\":[\"Enter Airbnb\",\"Show result\",\"Capture evidence\"],\"success\":\"Airbnb is visible in the result\"}]}"),
		tool("type_text", map[string]any{"ref": "e1-2", "text": "Airbnb"}),
		tool("click", map[string]any{"ref": "e2-4"}),
		tool("screenshot", map[string]any{"label": "Integration result"}),
		response(fmt.Sprintf("{\"cases\":[{\"id\":\"T1\",\"status\":\"passed\",\"steps\":[\"Entered Airbnb\",\"Displayed result\"],\"findings\":[],\"evidence\":[%q]}],\"summary\":\"Company search passed\"}", evidencePath)),
		response("{\"verdict\":\"passed\",\"summary\":\"Company search was positively verified\",\"findings\":[],\"recommendations\":[]}"),
	}}
	runner := New(db, browser.NewPlaywrightFactory(), Options{
		ScreenshotDir: filepath.Join(t.TempDir(), "screenshots"),
		Timeout:       30 * time.Second,
		Provider:      provider,
	})
	runner.Run(context.Background(), run.ID, domain.RunAuthorization{})

	current, err := db.GetRun(run.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != domain.RunStatusPassed || current.Report == nil || current.Report.Verdict != domain.ReportVerdictPassed {
		t.Fatalf("run = %#v", current)
	}
	var typeActions, clickActions, screenshots int
	for _, event := range current.Events {
		switch {
		case event.Type == domain.EventBrowserAction && event.Data["tool"] == "type_text":
			typeActions++
		case event.Type == domain.EventBrowserAction && event.Data["tool"] == "click":
			clickActions++
		case event.Type == domain.EventBrowserScreenshot:
			screenshots++
		}
	}
	if typeActions != 1 || clickActions != 1 || screenshots != 3 {
		t.Fatalf("actions/screenshots = %d/%d/%d", typeActions, clickActions, screenshots)
	}
}
