package browser_test

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ace-foundry/argus-testing/argus/internal/browser"
)

func TestPlaywrightRequestPolicyBlocksUnauthorizedTraffic(t *testing.T) {
	if os.Getenv("ARGUS_PLAYWRIGHT_SMOKE") != "1" {
		t.Skip("set ARGUS_PLAYWRIGHT_SMOKE=1 after running argus install-browser")
	}
	var posts atomic.Int32
	var unauthorizedHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		unauthorizedHits.Add(1)
		_, _ = fmt.Fprint(w, "unauthorized")
	}))
	defer target.Close()
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/save" {
			posts.Add(1)
			_, _ = fmt.Fprint(w, "saved")
			return
		}
		_, _ = fmt.Fprintf(w, "<!doctype html><button onclick=\"fetch('/save',{method:'POST'}).then(()=>this.textContent='saved').catch(()=>this.textContent='blocked')\">Save</button><a href=%q>Leave</a>", target.URL)
	}))
	defer app.Close()

	session, err := browser.NewPlaywrightFactory().Open(context.Background(), browser.SessionOptions{
		AllowNavigation: func(value string) bool { return strings.HasPrefix(value, app.URL) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Navigate(context.Background(), app.URL); err != nil {
		t.Fatal(err)
	}
	page, err := session.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Click(context.Background(), elementRef(t, page, "Save")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Wait(context.Background(), browser.WaitCondition{Text: "blocked", TimeoutMillis: 1_000}); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 0 {
		t.Fatalf("read-only session sent %d POST requests", posts.Load())
	}
	page, err = session.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Click(context.Background(), elementRef(t, page, "Leave")); !errors.Is(err, browser.ErrNavigationBlocked) {
		t.Fatalf("cross-origin navigation error = %v", err)
	}
	after, err := session.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if unauthorizedHits.Load() != 0 || !strings.HasPrefix(after.URL, app.URL) {
		t.Fatalf("unauthorized navigation reached target: hits=%d url=%q", unauthorizedHits.Load(), after.URL)
	}
	if networkErrors := mustNetworkErrors(t, session); len(networkErrors) == 0 {
		t.Fatal("blocked requests were not observable")
	}

	mutatingSession, err := browser.NewPlaywrightFactory().Open(context.Background(), browser.SessionOptions{
		AllowMutations:  true,
		AllowNavigation: func(value string) bool { return strings.HasPrefix(value, app.URL) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mutatingSession.Close()
	if _, err := mutatingSession.Navigate(context.Background(), app.URL); err != nil {
		t.Fatal(err)
	}
	page, err = mutatingSession.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutatingSession.Click(context.Background(), elementRef(t, page, "Save")); err != nil {
		t.Fatal(err)
	}
	if _, err := mutatingSession.Wait(context.Background(), browser.WaitCondition{Text: "saved", TimeoutMillis: 1_000}); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 {
		t.Fatalf("mutation-authorized session sent %d POST requests", posts.Load())
	}
}

func TestPlaywrightMasksSensitiveInputInScreenshots(t *testing.T) {
	if os.Getenv("ARGUS_PLAYWRIGHT_SMOKE") != "1" {
		t.Skip("set ARGUS_PLAYWRIGHT_SMOKE=1 after running argus install-browser")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "<style>body{margin:0}input{position:absolute;left:10px;top:10px;width:180px;height:30px;background:white}</style><input aria-label=\"API token\">")
	}))
	defer server.Close()
	session, err := browser.NewPlaywrightFactory().Open(context.Background(), browser.SessionOptions{})
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
	if _, err := session.Type(context.Background(), elementRef(t, page, "API token"), browser.InputValue{Text: "private-token", Sensitive: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "masked.png")
	if err := session.Screenshot(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	image, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	red, green, blue, _ := image.At(50, 25).RGBA()
	if red < 0xd000 || green > 0x2000 || blue < 0xd000 {
		t.Fatalf("sensitive mask pixel = %#x %#x %#x", red, green, blue)
	}
}

func mustNetworkErrors(t *testing.T, session browser.Session) []browser.NetworkError {
	t.Helper()
	values, err := session.NetworkErrors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return values
}

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
	if _, err := session.Type(context.Background(), search, browser.InputValue{Text: "Airbnb"}); err != nil {
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
<button id="network" onclick="fetch('/missing').finally(()=>document.querySelector('#network-state').textContent='network done')">Network error</button>
<span id="network-state"></span>
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

	if _, err := session.FillForm(context.Background(), map[string]browser.InputValue{last: {Text: "Lovelace"}, first: {Text: "Ada"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Type(context.Background(), keys, browser.InputValue{Text: "x"}); err != nil {
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
	if _, err := session.Wait(context.Background(), browser.WaitCondition{Text: "network done", TimeoutMillis: 1_000}); err != nil {
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
