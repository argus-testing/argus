package gemini_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/ace-foundry/argus-testing/backend-go/internal/agent"
	"github.com/ace-foundry/argus-testing/backend-go/internal/gemini"
)

func TestStreamMapsPayloadAndPreservesWireParts(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Fatalf("API key = %q", r.Header.Get("x-goog-api-key"))
		}
		if r.URL.Path != "/v1beta/models/gemini-test:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
			t.Fatalf("request URL = %s", r.URL)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		assertPayload(t, payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hel\"},{\"functionCall\":{\"name\":\"read_page\",\"args\":{\"url\":\"https://example.test\"}},\"thoughtSignature\":\"sig\"}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := gemini.New("test-key", gemini.WithBaseURL(server.URL), gemini.WithHTTPClient(server.Client()))
	var events []agent.ModelEvent
	err := provider.Stream(context.Background(), request(), func(event agent.ModelEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %#v", events)
	}
	if delta, ok := events[0].(agent.TextDelta); !ok || delta.Text != "Hel" {
		t.Fatalf("first event = %#v", events[0])
	}
	if delta, ok := events[1].(agent.TextDelta); !ok || delta.Text != "lo" {
		t.Fatalf("second event = %#v", events[1])
	}
	final, ok := events[2].(agent.ModelResponse)
	if !ok || len(final.Parts) != 2 || final.Parts[0].Text.Text != "Hello" || final.Parts[1].ToolCall.CallID != "gemini-1" || final.Parts[1].ToolCall.ProviderData["thoughtSignature"] != "sig" {
		t.Fatalf("final event = %#v", events[2])
	}
}

func TestStreamNormalizesStringMapToolResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		contents := payload["contents"].([]any)
		response := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)["response"]
		if want := map[string]any{"text": "page"}; !reflect.DeepEqual(response, want) {
			t.Fatalf("function response = %#v, want %#v", response, want)
		}
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n"))
	}))
	defer server.Close()

	modelRequest := request()
	modelRequest.Messages[3].Parts[0].ToolResult.Result = map[string]string{"text": "page"}
	provider := gemini.New("key", gemini.WithBaseURL(server.URL))
	if err := provider.Stream(context.Background(), modelRequest, nil); err != nil {
		t.Fatal(err)
	}
}

func TestStreamRateLimitAndInvalidFinalResponse(t *testing.T) {
	t.Parallel()
	for name, handler := range map[string]http.HandlerFunc{
		"rate limit": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
		},
		"no final": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		},
		"malformed": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {not json}\n\n"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			provider := gemini.New("key", gemini.WithBaseURL(server.URL))
			err := provider.Stream(context.Background(), request(), func(agent.ModelEvent) error { return nil })
			if name == "rate limit" {
				var limited *gemini.RateLimitError
				if !errors.As(err, &limited) || limited.StatusCode != http.StatusTooManyRequests || limited.RetryAfter != 2*time.Second {
					t.Fatalf("error = %#v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func request() agent.ModelRequest {
	return agent.ModelRequest{
		Model: agent.ModelRef{Provider: "gemini", Model: "gemini-test"},
		Messages: []agent.Message{
			{Role: agent.RoleSystem, Parts: []agent.MessagePart{{Text: &agent.TextPart{Text: "system"}}}},
			{Role: agent.RoleUser, Parts: []agent.MessagePart{{Text: &agent.TextPart{Text: "hello"}}, {Image: &agent.ImagePart{Data: []byte("png"), MediaType: "image/png"}}, {Audio: &agent.AudioPart{Data: []byte("mp3"), MediaType: "audio/mpeg"}}}},
			{Role: agent.RoleAssistant, Parts: []agent.MessagePart{{ToolCall: &agent.ToolCallPart{CallID: "call-1", Name: "read_page", Arguments: map[string]any{}, ProviderData: map[string]any{"thoughtSignature": "old-sig"}}}}},
			{Role: agent.RoleTool, Parts: []agent.MessagePart{{ToolResult: &agent.ToolResultPart{CallID: "call-1", Name: "read_page", Result: map[string]any{"text": "page"}}}}},
		},
		Tools:      []agent.Tool{{Name: "read_page", Description: "Read a page.", InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`)}},
		Generation: agent.GenerationOptions{Temperature: ptr(0.25), MaxOutputTokens: ptr(123), JSONMode: true},
	}
}

func ptr[T any](value T) *T { return &value }

func assertPayload(t *testing.T, got map[string]any) {
	t.Helper()
	want := map[string]any{
		"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "system"}}},
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}, map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "cG5n"}}, map[string]any{"inlineData": map[string]any{"mimeType": "audio/mpeg", "data": "bXAz"}}}},
			map[string]any{"role": "model", "parts": []any{map[string]any{"functionCall": map[string]any{"id": "call-1", "name": "read_page", "args": map[string]any{}}, "thoughtSignature": "old-sig"}}},
			map[string]any{"role": "user", "parts": []any{map[string]any{"functionResponse": map[string]any{"id": "call-1", "name": "read_page", "response": map[string]any{"text": "page"}}}}},
		},
		"tools":            []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "read_page", "description": "Read a page.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"url": map[string]any{"type": "string"}}, "required": []any{"url"}}}}}},
		"generationConfig": map[string]any{"temperature": 0.25, "maxOutputTokens": float64(123), "responseMimeType": "application/json"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %#v\nwant %#v", got, want)
	}
}
