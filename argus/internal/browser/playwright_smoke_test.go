package browser_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ace-foundry/argus-testing/argus/internal/browser"
)

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
