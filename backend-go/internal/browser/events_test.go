package browser_test

import (
	"testing"

	"github.com/ace-foundry/argus-testing/backend-go/internal/browser"
)

func TestEventFormattersProtectBrowserPrivacy(t *testing.T) {
	action := browser.FormatAction("type_text", map[string]any{"selector": "#password", "text": "secret"})
	arguments := action["arguments"].(map[string]any)
	if got := arguments["text"]; got != "[redacted]" {
		t.Fatalf("type_text = %#v", got)
	}

	navigate := browser.FormatAction("navigate", map[string]any{"url": "https://example.com/a?token=secret&view=all"})
	if got := navigate["arguments"].(map[string]any)["url"]; got != "https://example.com/a?view=all" {
		t.Fatalf("navigate URL = %#v", got)
	}

	observation := browser.FormatObservation("inspect_page", map[string]any{"body": "never persist this"})
	result := observation["result"].(map[string]any)
	if result["omitted"] != true || result["body"] != nil {
		t.Fatalf("inspect result = %#v", result)
	}
}
