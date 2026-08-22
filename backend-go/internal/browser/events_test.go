package browser_test

import (
	"reflect"
	"testing"

	"github.com/ace-foundry/argus-testing/backend-go/internal/browser"
)

func TestEventFormattersProtectBrowserPrivacy(t *testing.T) {
	url := "https://example.com/a?token=secret&view=all"
	for _, test := range []struct {
		name string
		got  map[string]any
		want map[string]any
	}{
		{"navigate action", browser.FormatAction("navigate", map[string]any{"url": url}), map[string]any{"tool": "navigate", "arguments": map[string]any{"url": "https://example.com/a?view=all"}}},
		{"navigate observation", browser.FormatObservation("navigate", map[string]any{"url": url, "title": "Example", "body": "never persist this"}), map[string]any{"tool": "navigate", "result": map[string]any{"url": "https://example.com/a?view=all", "title": "Example"}}},
		{"click observation", browser.FormatObservation("click", map[string]any{"url": "https://example.com/a?token=secret", "result": "clicked", "body": "never persist this"}), map[string]any{"tool": "click", "result": map[string]any{"url": "https://example.com/a", "result": "clicked"}}},
		{"inspect observation", browser.FormatObservation("inspect_page", map[string]any{"body": "never persist this", "interactive": "never persist this either"}), map[string]any{"tool": "inspect_page", "result": map[string]any{"omitted": true, "summary": "Page inspection omitted from persisted events"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.got, test.want) {
				t.Fatalf("event = %#v, want %#v", test.got, test.want)
			}
		})
	}
}
