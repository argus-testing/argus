package runner

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ace-foundry/argus-testing/backend-go/internal/browser"
	"github.com/ace-foundry/argus-testing/backend-go/internal/domain"
	"github.com/ace-foundry/argus-testing/backend-go/internal/store"
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
	navigateErr    error
	screenshotErr  error
	closed         int
	screenshotPath string
}

func (s *fakeSession) Navigate(context.Context, string) (browser.Navigation, error) {
	return browser.Navigation{URL: "https://example.com", Title: "Example"}, s.navigateErr
}
func (s *fakeSession) Inspect(context.Context) (browser.Inspection, error) {
	return browser.Inspection{}, nil
}
func (s *fakeSession) Click(context.Context, string) error        { return nil }
func (s *fakeSession) Fill(context.Context, string, string) error { return nil }
func (s *fakeSession) Screenshot(_ context.Context, path string) error {
	s.screenshotPath = path
	if s.screenshotErr != nil {
		return s.screenshotErr
	}
	return os.WriteFile(path, []byte("png"), 0o644)
}
func (s *fakeSession) Close() error { s.closed++; return nil }

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
	run, err := db.CreateRun("https://example.com", "check")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddEvent(run.ID, domain.EventRunQueued, nil); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestRunnerCapturesEvidenceThenFailsWithoutPipeline(t *testing.T) {
	db, databasePath := newTestStore(t)
	run := queuedRun(t, db)
	session := &fakeSession{}
	var published []domain.RunEvent
	r := New(db, fakeFactory{session: session}, Options{ScreenshotDir: filepath.Join(t.TempDir(), "screenshots")})
	r.SetPublisher(func(event domain.RunEvent) { published = append(published, event) })

	r.Run(context.Background(), run.ID)

	current, err := db.GetRun(run.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != domain.RunStatusFailed || current.Error == nil || *current.Error != pipelineNotConfigured {
		t.Fatalf("run = %#v", current)
	}
	if session.closed != 1 {
		t.Fatalf("close calls = %d", session.closed)
	}
	if _, err := os.Stat(session.screenshotPath); err != nil {
		t.Fatalf("screenshot was not saved: %v", err)
	}
	want := []domain.EventType{domain.EventRunQueued, domain.EventRunStarted, domain.EventBrowserScreenshot, domain.EventRunFailed}
	if len(current.Events) != len(want) {
		t.Fatalf("events = %#v", current.Events)
	}
	if want := "/screenshots/" + run.ID + "/initial-1.png"; current.Events[2].Data["path"] != want {
		t.Fatalf("screenshot event = %#v", current.Events[2])
	}
	reader, err := sql.Open("sqlite", "file:"+databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var screenshotCount int
	if err := reader.QueryRow("SELECT COUNT(*) FROM screenshots WHERE run_id = ?", run.ID).Scan(&screenshotCount); err != nil || screenshotCount != 1 {
		t.Fatalf("screenshot persistence = %d, %v", screenshotCount, err)
	}
	for index, event := range current.Events {
		if event.Type != want[index] {
			t.Fatalf("event %d = %s, want %s", index, event.Type, want[index])
		}
	}
	if len(published) != 3 || published[0].Type != domain.EventRunStarted || published[1].Type != domain.EventBrowserScreenshot || published[2].Type != domain.EventRunFailed {
		t.Fatalf("published = %#v", published)
	}
}

func TestRunnerClosesContextAfterBrowserFailure(t *testing.T) {
	db, _ := newTestStore(t)
	run := queuedRun(t, db)
	session := &fakeSession{navigateErr: errors.New("navigation failed")}
	r := New(db, fakeFactory{session: session}, Options{ScreenshotDir: t.TempDir()})

	r.Run(context.Background(), run.ID)

	if session.closed != 1 {
		t.Fatalf("close calls = %d", session.closed)
	}
	current, err := db.GetRun(run.ID, false)
	if err != nil || current.Status != domain.RunStatusFailed {
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
	r := New(db, fakeFactory{open: func(ctx context.Context) error {
		once.Do(func() { close(opened) })
		<-ctx.Done()
		return ctx.Err()
	}}, Options{ScreenshotDir: t.TempDir()})
	done := make(chan struct{})
	go func() { r.Run(ctx, run.ID); close(done) }()
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
	if got := current.Events[len(current.Events)-1].Type; got != domain.EventRunCancelled {
		t.Fatalf("last event = %s", got)
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
