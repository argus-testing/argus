package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ace-foundry/argus-testing/argus/internal/domain"
	"github.com/coder/websocket"
)

type validationResponse struct {
	Detail []validationError `json:"detail"`
}

func TestStaticScreenshotsAndSymlinkContainment(t *testing.T) {
	staticDir := t.TempDir()
	screenshotDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("app"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(screenshotDir, "shot.png"), []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, _, _ := newTestServer(t, staticDir)
	server.screenshotDir = screenshotDir
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()

	response := request(t, httpServer.Client(), http.MethodGet, httpServer.URL+"/screenshots/shot.png", "")
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || string(body) != "image" {
		t.Fatalf("screenshot = %d %q %v", response.StatusCode, body, err)
	}

	external := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(external, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(staticDir, "escape.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	response = request(t, httpServer.Client(), http.MethodGet, httpServer.URL+"/escape.txt", "")
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "app" {
		t.Fatalf("symlink escape = %d %q", response.StatusCode, body)
	}
}

func TestWebSocketDeliversMoreThanChannelCapacityAndTerminal(t *testing.T) {
	server, db, _ := newTestServer(t, "")
	run, err := db.CreateRun("https://example.com", "check", domain.RunPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := db.AddEvent(run.ID, domain.EventRunQueued, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws/runs/"+url.PathEscape(run.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, message, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var replay domain.RunEvent
	if err := json.Unmarshal(message, &replay); err != nil || replay.ID != queued.ID {
		t.Fatalf("replay = %#v, %v", replay, err)
	}

	producerDone := make(chan error, 1)
	go func() {
		for range 130 {
			event, err := db.AddEvent(run.ID, domain.EventBrowserAction, nil)
			if err != nil {
				producerDone <- err
				return
			}
			server.Publish(*event)
		}
		terminal, err := db.Transition(run.ID, []domain.RunStatus{domain.RunStatusQueued}, domain.RunStatusPassed, domain.EventRunCompleted, nil, nil, nil)
		if err == nil {
			server.Publish(*terminal)
		}
		producerDone <- err
	}()

	terminalSeen := false
	for range 131 {
		_, message, err = conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var event domain.RunEvent
		if err := json.Unmarshal(message, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == domain.EventRunCompleted {
			terminalSeen = true
		}
	}
	if err := <-producerDone; err != nil {
		t.Fatal(err)
	}
	if !terminalSeen {
		t.Fatal("terminal event was not delivered")
	}
	_, _, err = conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("close = %v", err)
	}
}

func TestSettingsUsesConfiguredModel(t *testing.T) {
	_, db, _ := newTestServer(t, "")
	server, err := New(db, nil, Options{Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	response := request(t, httpServer.Client(), http.MethodGet, httpServer.URL+"/api/settings", "")
	var settings domain.SettingsResponse
	decode(t, response, &settings)
	if settings.Model != "gemini-test" {
		t.Fatalf("model = %q", settings.Model)
	}
}

func TestSanitizedURLAndValidationErrorDetails(t *testing.T) {
	server, _, _ := newTestServer(t, "")
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	client := httpServer.Client()
	response := request(t, client, http.MethodPost, httpServer.URL+"/api/runs", `{"url":"HTTPS://EXAMPLE.com:080/a%20b?view=two%20words&empty#frag","instructions":"check"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", response.StatusCode)
	}
	var run domain.Run
	decode(t, response, &run)
	if run.URL != "https://example.com:80/a%20b?view=two+words&empty=#frag" {
		t.Fatalf("stored URL = %q", run.URL)
	}
	for _, test := range []struct{ input, expected string }{
		{"https://example.com/%ZZ", "https://example.com/%ZZ"},
		{"https://example.com/?x=%ZZ", "https://example.com/?x=%25ZZ"},
		{"https://example.com/#%ZZ", "https://example.com/#%ZZ"},
	} {
		response = request(t, client, http.MethodPost, httpServer.URL+"/api/runs", `{"url":"`+test.input+`","instructions":"check"}`)
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("create %q = %d", test.input, response.StatusCode)
		}
		decode(t, response, &run)
		if run.URL != test.expected {
			t.Fatalf("stored URL for %q = %q, want %q", test.input, run.URL, test.expected)
		}
	}

	for _, test := range []struct {
		target, body string
		loc          []any
	}{
		{httpServer.URL + "/api/runs", `{"url":`, []any{"body"}},
		{httpServer.URL + "/api/runs", `{"url":"https://example.com"}`, []any{"body", "instructions"}},
		{httpServer.URL + "/api/runs", `{"url":7,"instructions":"check"}`, []any{"body", "url"}},
		{httpServer.URL + "/api/runs", `{"url":"https://example.com","instructions":""}`, []any{"body", "instructions"}},
		{httpServer.URL + "/api/runs?limit=bad", "", []any{"query", "limit"}},
	} {
		response = request(t, client, http.MethodPost, test.target, test.body)
		if strings.Contains(test.target, "limit=") {
			response.Body.Close()
			response = request(t, client, http.MethodGet, test.target, "")
		}
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status for %s = %d", test.target, response.StatusCode)
		}
		var validation validationResponse
		decode(t, response, &validation)
		if len(validation.Detail) == 0 || !sameLoc(validation.Detail[0].Loc, test.loc) || validation.Detail[0].Type == "" || validation.Detail[0].Msg == "" {
			t.Fatalf("validation = %#v", validation)
		}
	}
}

func sameLoc(actual, expected []any) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}
