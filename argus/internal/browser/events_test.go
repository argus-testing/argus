package browser_test

import (
	"reflect"
	"testing"

	"github.com/ace-foundry/argus-testing/argus/internal/browser"
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
		{"click action", browser.FormatAction("click", map[string]any{"ref": "e1-2", "text": "never persist this"}), map[string]any{"tool": "click", "arguments": map[string]any{"ref": "e1-2"}}},
		{"type action", browser.FormatAction("type_text", map[string]any{"ref": "e1-3", "text": "never persist this", "secret": "binding_name"}), map[string]any{"tool": "type_text", "arguments": map[string]any{"ref": "e1-3"}}},
		{"fill action", browser.FormatAction("fill_form", map[string]any{"fields": []any{map[string]any{"ref": "e1-3", "text": "private"}, map[string]any{"ref": "e1-4", "secret": "binding_name"}}}), map[string]any{"tool": "fill_form", "arguments": map[string]any{"refs": []string{"e1-3", "e1-4"}}}},
		{"click observation", browser.FormatObservation("click", map[string]any{"url": "https://example.com/a?token=secret", "action": "clicked", "snapshot": map[string]any{"text": "never persist this"}}), map[string]any{"tool": "click", "result": map[string]any{"url": "https://example.com/a", "action": "clicked"}}},
		{"inspect observation", browser.FormatObservation("inspect_page", map[string]any{"body": "never persist this", "interactive": "never persist this either"}), map[string]any{"tool": "inspect_page", "result": map[string]any{"omitted": true, "summary": "Page inspection omitted from persisted events"}}},
		{"diagnostic observation", browser.FormatObservation("console_errors", map[string]any{"errors": []string{"private page error"}}), map[string]any{"tool": "console_errors", "result": map[string]any{"omitted": true, "summary": "Browser diagnostics omitted from persisted events"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.got, test.want) {
				t.Fatalf("event = %#v, want %#v", test.got, test.want)
			}
		})
	}
}
