package store

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/ace-foundry/argus-testing/backend-go/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "argus.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestCRUDAndJSONPersistence(t *testing.T) {
	store := newTestStore(t)
	run, err := store.CreateRun("https://example.com", "check")
	if err != nil {
		t.Fatal(err)
	}
	event, err := store.AddEvent(run.ID, domain.EventRunQueued, map[string]any{"nested": map[string]any{"ok": true}})
	if err != nil {
		t.Fatal(err)
	}
	report := &domain.RunReport{Verdict: domain.ReportVerdictPassed, Summary: "ok", Findings: []domain.Finding{}, Recommendations: []string{"ship"}}
	if _, err := store.Transition(run.ID, []domain.RunStatus{domain.RunStatusQueued}, domain.RunStatusPassed, domain.EventRunCompleted, map[string]any{"verdict": "passed"}, report, nil); err != nil {
		t.Fatal(err)
	}
	if screenshotID, err := store.AddScreenshot(run.ID, "screenshots/example.png"); err != nil || screenshotID == "" {
		t.Fatalf("screenshot = %q, %v", screenshotID, err)
	}
	if _, err := store.AddScreenshot("missing", "screenshots/missing.png"); err == nil {
		t.Fatal("foreign key constraint was not enforced")
	}

	got, err := store.GetRun(run.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status != domain.RunStatusPassed || got.Report == nil || got.Report.Summary != "ok" {
		t.Fatalf("run = %#v", got)
	}
	if len(got.Events) != 2 || got.Events[0].ID != event.ID || got.Events[0].Data["nested"] == nil {
		t.Fatalf("events = %#v", got.Events)
	}
	if got.CreatedAt[len(got.CreatedAt)-6:] != "+00:00" {
		t.Fatalf("timestamp = %q", got.CreatedAt)
	}
}

func TestCompetingTerminalTransitionsAreAtomic(t *testing.T) {
	store := newTestStore(t)
	run, err := store.CreateRun("https://example.com", "check")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(run.ID, []domain.RunStatus{domain.RunStatusQueued}, domain.RunStatusRunning, domain.EventRunStarted, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make(chan *domain.RunEvent, 2)
	for _, next := range []struct {
		status domain.RunStatus
		event  domain.EventType
	}{{domain.RunStatusPassed, domain.EventRunCompleted}, {domain.RunStatusCancelled, domain.EventRunCancelled}} {
		wg.Add(1)
		go func(next struct {
			status domain.RunStatus
			event  domain.EventType
		}) {
			defer wg.Done()
			event, err := store.Transition(run.ID, []domain.RunStatus{domain.RunStatusRunning}, next.status, next.event, nil, nil, nil)
			if err != nil {
				t.Error(err)
			}
			results <- event
		}(next)
	}
	wg.Wait()
	close(results)
	count := 0
	for event := range results {
		if event != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("terminal transitions = %d", count)
	}
	got, err := store.GetRun(run.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.RunStatusPassed && got.Status != domain.RunStatusCancelled {
		t.Fatalf("status = %s", got.Status)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events = %#v", got.Events)
	}
}

func TestReconcileInterrupted(t *testing.T) {
	store := newTestStore(t)
	queued, _ := store.CreateRun("https://example.com/queued", "check")
	running, _ := store.CreateRun("https://example.com/running", "check")
	if _, err := store.Transition(running.ID, []domain.RunStatus{domain.RunStatusQueued}, domain.RunStatusRunning, domain.EventRunStarted, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	events, err := store.ReconcileInterrupted()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %#v", events)
	}
	for _, id := range []string{queued.ID, running.ID} {
		run, err := store.GetRun(id, true)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != domain.RunStatusFailed || run.Error == nil || *run.Error != "Run interrupted by server restart" || run.Events[len(run.Events)-1].Type != domain.EventRunFailed {
			t.Fatalf("run = %#v", run)
		}
	}
}
