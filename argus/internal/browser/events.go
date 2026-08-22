package browser

import (
	"strings"
	"unicode/utf8"

	"github.com/ace-foundry/argus-testing/argus/internal/server"
)

// FormatAction returns the exact event shape safe to persist for browser tool calls.
func FormatAction(tool string, arguments map[string]any) map[string]any {
	persisted := map[string]any{}
	switch tool {
	case "navigate":
		if url, ok := arguments["url"].(string); ok {
			persisted["url"] = server.SanitizeURL(url)
		} else {
			persisted["url"] = "[invalid URL]"
		}
	case "click":
		persisted["ref"] = boundedString(arguments["ref"], 100)
	case "screenshot":
		persisted["label"] = boundedString(arguments["label"], 80)
	}
	return map[string]any{"tool": tool, "arguments": persisted}
}

// FormatObservation omits page content and persists only bounded public results.
func FormatObservation(tool string, result any) map[string]any {
	if tool == "inspect_page" {
		return map[string]any{"tool": tool, "result": map[string]any{"omitted": true, "summary": "Page inspection omitted from persisted events"}}
	}
	values, _ := result.(map[string]any)
	persisted := map[string]any{}
	switch tool {
	case "navigate":
		if url, ok := values["url"].(string); ok {
			persisted["url"] = server.SanitizeURL(url)
		}
		persisted["title"] = boundedString(values["title"], 1000)
	case "click":
		if url, ok := values["url"].(string); ok {
			persisted["url"] = server.SanitizeURL(url)
		}
		persisted["result"] = boundedString(values["result"], 80)
	case "screenshot":
		persisted["path"] = boundedString(values["path"], 500)
		persisted["label"] = boundedString(values["label"], 80)
	}
	return map[string]any{"tool": tool, "result": persisted}
}

func boundedString(value any, maximum int) string {
	text, _ := value.(string)
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= maximum {
		return text
	}
	return string([]rune(text)[:maximum])
}
