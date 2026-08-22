package browser

import "github.com/ace-foundry/argus-testing/backend-go/internal/server"

const redacted = "[redacted]"

// FormatAction returns event data safe to persist for a future browser tool call.
func FormatAction(tool string, arguments map[string]any) map[string]any {
	persisted := make(map[string]any, len(arguments)+1)
	for key, value := range arguments {
		persisted[key] = value
	}
	if tool == "type_text" {
		if _, ok := persisted["text"]; ok {
			persisted["text"] = redacted
		}
	}
	if tool == "navigate" {
		url, ok := persisted["url"].(string)
		if !ok {
			persisted["url"] = "[invalid URL]"
		} else {
			persisted["url"] = server.SanitizeURL(url)
		}
	}
	return map[string]any{"tool": tool, "arguments": persisted}
}

// FormatObservation omits page contents from persisted events.
func FormatObservation(tool string, result any) map[string]any {
	if tool == "inspect_page" {
		return map[string]any{"tool": tool, "result": map[string]any{"omitted": true, "summary": "Page inspection omitted from persisted events"}}
	}
	if values, ok := result.(map[string]any); ok {
		persisted := make(map[string]any, len(values))
		for key, value := range values {
			persisted[key] = value
		}
		if url, ok := persisted["url"].(string); ok {
			persisted["url"] = server.SanitizeURL(url)
		}
		return map[string]any{"tool": tool, "result": persisted}
	}
	return map[string]any{"tool": tool, "result": result}
}
