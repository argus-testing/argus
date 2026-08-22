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
	case "click", "type_text", "submit_form", "select_option":
		persisted["ref"] = boundedString(arguments["ref"], 100)
	case "fill_form":
		fields, _ := arguments["fields"].([]any)
		references := make([]string, 0, min(len(fields), 20))
		for _, value := range fields {
			field, _ := value.(map[string]any)
			if reference := boundedString(field["ref"], 100); reference != "" && len(references) < 20 {
				references = append(references, reference)
			}
		}
		persisted["refs"] = references
	case "press_key":
		persisted["key"] = boundedString(arguments["key"], 100)
	case "scroll":
		persisted["delta_y"] = arguments["delta_y"]
	case "resize_viewport":
		persisted["width"] = arguments["width"]
		persisted["height"] = arguments["height"]
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
	if tool == "console_errors" || tool == "network_errors" {
		return map[string]any{"tool": tool, "result": map[string]any{"omitted": true, "summary": "Browser diagnostics omitted from persisted events"}}
	}
	values, _ := result.(map[string]any)
	persisted := map[string]any{}
	switch tool {
	case "navigate":
		if url, ok := values["url"].(string); ok {
			persisted["url"] = server.SanitizeURL(url)
		}
		persisted["title"] = boundedString(values["title"], 1000)
	case "click", "type_text", "fill_form", "submit_form", "select_option", "press_key", "scroll", "resize_viewport", "wait_for":
		if url, ok := values["url"].(string); ok {
			persisted["url"] = server.SanitizeURL(url)
		}
		persisted["action"] = boundedString(values["action"], 80)
	case "visual_click":
		if url, ok := values["url"].(string); ok {
			persisted["url"] = server.SanitizeURL(url)
		}
		persisted["action"] = boundedString(values["action"], 80)
		persisted["path"] = boundedString(values["path"], 500)
	case "find_elements":
		persisted["path"] = boundedString(values["path"], 500)
		persisted["width"] = values["width"]
		persisted["height"] = values["height"]
		if matches, ok := values["matches"].([]any); ok {
			persisted["match_count"] = min(len(matches), 10)
		}
	case "screenshot":
		persisted["path"] = boundedString(values["path"], 500)
		persisted["label"] = boundedString(values["label"], 80)
	}
	return map[string]any{"tool": tool, "result": persisted}
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func boundedString(value any, maximum int) string {
	text, _ := value.(string)
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= maximum {
		return text
	}
	return string([]rune(text)[:maximum])
}
