package browser_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ace-foundry/argus-testing/argus/internal/browser"
)

func TestPlaywrightSemanticInteractionSurface(t *testing.T) {
	if os.Getenv("ARGUS_PLAYWRIGHT_SMOKE") != "1" {
		t.Skip("set ARGUS_PLAYWRIGHT_SMOKE=1 after running argus install-browser")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<!doctype html><html><body style="min-height:1600px">
<label for="q">Search</label><input id="q" placeholder="Company">
<label for="batch">Batch</label><select id="batch"><option>All</option><option>Winter 2024</option></select>
<button id="show" onclick="document.querySelector('#result').textContent=document.querySelector('#q').value+' '+document.querySelector('#batch').value">Show</button>
<div id="result"></div></body></html>`)
	}))
	defer server.Close()

	session, err := browser.NewPlaywrightFactory().Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	page, err := session.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	search := elementRef(t, page, "Search")
	batch := elementRef(t, page, "Batch")
	show := elementRef(t, page, "Show")
	if _, err := session.Type(context.Background(), search, "Airbnb"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Select(context.Background(), batch, "Winter 2024"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Click(context.Background(), show); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Wait(context.Background(), browser.WaitCondition{Text: "Airbnb Winter 2024", TimeoutMillis: 1000}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Scroll(context.Background(), 600); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Resize(context.Background(), 375, 812); err != nil {
		t.Fatal(err)
	}
	after, err := session.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.Width != 375 || after.Height != 812 || !strings.Contains(after.Text, "Airbnb Winter 2024") {
		t.Fatalf("snapshot = %#v", after)
	}
}

func TestPlaywrightFormAndDiagnosticSurface(t *testing.T) {
	if os.Getenv("ARGUS_PLAYWRIGHT_SMOKE") != "1" {
		t.Skip("set ARGUS_PLAYWRIGHT_SMOKE=1 after running argus install-browser")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/submitted":
			_, _ = fmt.Fprintf(w, "<title>Submitted</title><p>%s %s</p>", request.URL.Query().Get("first"), request.URL.Query().Get("last"))
		case "/missing":
			http.Error(w, "missing", http.StatusNotFound)
		default:
			_, _ = fmt.Fprint(w, `<!doctype html><html><body>
<form action="/submitted" method="get">
  <label for="first">First name</label><input id="first" name="first">
  <label for="last">Last name</label><input id="last" name="last">
  <button type="submit">Send</button>
</form>
<label for="keys">Keys</label><input id="keys" onkeydown="document.querySelector('#pressed').textContent=event.key">
<span id="pressed"></span>
<button id="console" onclick="console.error('fixture console failure')">Console error</button>
<button id="network" onclick="fetch('/missing')">Network error</button>
<button id="point" style="position:fixed;right:10px;bottom:10px;width:100px;height:50px" onclick="this.textContent='Point clicked'">Point target</button>
</body></html>`)
		}
	}))
	defer server.Close()

	session, err := browser.NewPlaywrightFactory().Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	page, err := session.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first := elementRef(t, page, "First name")
	last := elementRef(t, page, "Last name")
	keys := elementRef(t, page, "Keys")
	consoleButton := elementRef(t, page, "Console error")
	networkButton := elementRef(t, page, "Network error")
	submit := elementRef(t, page, "Send")

	if _, err := session.FillForm(context.Background(), map[string]string{last: "Lovelace", first: "Ada"}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Type(context.Background(), keys, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Press(context.Background(), "ArrowLeft"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Click(context.Background(), consoleButton); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Click(context.Background(), networkButton); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Wait(context.Background(), browser.WaitCondition{Text: "ArrowLeft", TimeoutMillis: 1_000}); err != nil {
		t.Fatal(err)
	}
	consoleErrors, err := session.ConsoleErrors(context.Background())
	if err != nil || !containsString(consoleErrors, "fixture console failure") {
		t.Fatalf("console errors = %#v, %v", consoleErrors, err)
	}
	if _, err := session.Wait(context.Background(), browser.WaitCondition{Text: "ArrowLeft", TimeoutMillis: 1_000}); err != nil {
		t.Fatal(err)
	}
	networkErrors, err := session.NetworkErrors(context.Background())
	if err != nil || len(networkErrors) != 1 || networkErrors[0].Status != http.StatusNotFound {
		t.Fatalf("network errors = %#v, %v", networkErrors, err)
	}

	if _, err := session.ClickPoint(context.Background(), page.Width-60, page.Height-35); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Wait(context.Background(), browser.WaitCondition{Text: "Point clicked", TimeoutMillis: 1_000}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Submit(context.Background(), submit); err != nil {
		t.Fatal(err)
	}
	after, err := session.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.Title != "Submitted" || !strings.Contains(after.Text, "Ada Lovelace") {
		t.Fatalf("submitted snapshot = %#v", after)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func elementRef(t *testing.T, snapshot browser.PageSnapshot, label string) string {
	t.Helper()
	for _, element := range snapshot.Elements {
		if element.Label == label || element.Name == label {
			return element.Ref
		}
	}
	t.Fatalf("element %q not found in %#v", label, snapshot.Elements)
	return ""
}

func TestPlaywrightSmoke(t *testing.T) {
	if os.Getenv("ARGUS_PLAYWRIGHT_SMOKE") != "1" {
		t.Skip("set ARGUS_PLAYWRIGHT_SMOKE=1 after running argus install-browser")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<title>Smoke</title><button>Go</button>"))
	}))
	defer server.Close()

	session, err := browser.NewPlaywrightFactory().Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := session.Navigate(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "smoke.png")
	if err := session.Screenshot(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
