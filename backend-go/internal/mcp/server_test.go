package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestProtocolHandshakeAndTools(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("unexpected HTTP request") }))
	responses := exchange(t, server, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":2,"method":"ping"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	if len(responses) != 3 {
		t.Fatalf("responses = %d, want 3", len(responses))
	}
	if got := responses[0]["result"].(map[string]any)["protocolVersion"]; got != "2025-03-26" {
		t.Fatalf("protocol version = %v", got)
	}
	if got := responses[1]["result"]; !reflect.DeepEqual(got, map[string]any{}) {
		t.Fatalf("ping = %#v", got)
	}
	tools := responses[2]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("tools = %d, want 5", len(tools))
	}
	want := []string{"start_test", "get_test_run", "list_test_runs", "cancel_test", "get_test_evidence"}
	for i, tool := range tools {
		value := tool.(map[string]any)
		if value["name"] != want[i] || value["description"] == "" || value["inputSchema"] == nil || value["outputSchema"] == nil {
			t.Fatalf("tool = %#v", value)
		}
	}
	start := tools[0].(map[string]any)["inputSchema"].(map[string]any)
	if !reflect.DeepEqual(start["required"], []any{"url", "instructions"}) {
		t.Fatalf("start schema = %#v", start)
	}
	if got := tools[0].(map[string]any)["outputSchema"]; !reflect.DeepEqual(got, map[string]any{"additionalProperties": true, "title": "start_testDictOutput", "type": "object"}) {
		t.Fatalf("start output schema = %#v", got)
	}
	if got := tools[2].(map[string]any)["outputSchema"]; !reflect.DeepEqual(got, map[string]any{"properties": map[string]any{"result": map[string]any{"items": map[string]any{"additionalProperties": true, "type": "object"}, "title": "Result", "type": "array"}}, "required": []any{"result"}, "title": "list_test_runsOutput", "type": "object"}) {
		t.Fatalf("list output schema = %#v", got)
	}
}

func TestMalformedRequestsReturnProtocolErrors(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	responses := exchange(t, server, `{bad`, `[]`, `{"jsonrpc":"2.0","id":1,"method":4}`, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`)
	if got := responses[0]["error"].(map[string]any)["code"]; got != float64(-32700) {
		t.Fatalf("parse error = %v", got)
	}
	if got := responses[1]["error"].(map[string]any)["code"]; got != float64(-32600) {
		t.Fatalf("non-object request = %v", got)
	}
	if got := responses[2]["error"].(map[string]any)["code"]; got != float64(-32600) {
		t.Fatalf("invalid request = %v", got)
	}
	if got := responses[3]["error"].(map[string]any)["code"]; got != float64(-32602) {
		t.Fatalf("invalid initialize params = %v", got)
	}
}

func TestInitializeRequiresClientContract(t *testing.T) {
	for _, params := range []string{
		`{"protocolVersion":"2025-11-25","capabilities":[],"clientInfo":{"name":"test","version":"1"}}`,
		`{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"","version":"1"}}`,
		`{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":""}}`,
	} {
		server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		response := exchange(t, server, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":`+params+`}`)[0]
		if got := response["error"].(map[string]any)["code"]; got != float64(-32602) {
			t.Fatalf("params %s: error = %v", params, got)
		}
	}
}

func TestToolHTTPMappingsAndResults(t *testing.T) {
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, r.Method+" "+r.URL.RequestURI()+" ")
		if r.Method == http.MethodPost && r.URL.Path == "/api/runs" {
			var payload map[string]string
			if err := json.Unmarshal(body, &payload); err != nil || !reflect.DeepEqual(payload, map[string]string{"url": "https://example.com", "instructions": "Check navigation"}) {
				t.Fatalf("start payload = %q, err = %v", body, err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/runs":
			if r.Method == http.MethodPost {
				_, _ = w.Write([]byte(`{"id":"run-1","status":"queued"}`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":"run-2","status":"passed"}]`))
		case "/api/runs/run-1/cancel":
			_, _ = w.Write([]byte(`{"id":"run-1","status":"cancelled"}`))
		default:
			_, _ = w.Write([]byte(`{"id":"run-1","status":"running","report":{"summary":"done"},"events":[{"type":"browser.observation","data":{"result":"PRIVATE PAGE"}},{"type":"browser.screenshot","created_at":"now","data":{"path":"/screenshots/run-1/final image.png","label":"Final"}},{"type":"browser.screenshot","data":{"path":"https://evil.example/stolen.png"}}]}`))
		}
	}))
	defer api.Close()
	server, err := New(api.URL, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	responses := exchange(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"start_test","arguments":{"url":"https://example.com","instructions":"Check navigation"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_test_run","arguments":{"run_id":"run-1"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_test_runs","arguments":{"limit":7}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"cancel_test","arguments":{"run_id":"run-1"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_test_evidence","arguments":{"run_id":"run-1"}}}`)
	wantRequests := []string{
		"POST /api/runs ", "GET /api/runs/run-1 ", "GET /api/runs?limit=7 ", "POST /api/runs/run-1/cancel ", "GET /api/runs/run-1 ",
	}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("requests = %#v", requests)
	}
	start := toolValue(t, responses[1])
	if start["run_id"] != "run-1" || !strings.Contains(start["polling_hint"].(string), "run-1") {
		t.Fatalf("start = %#v", start)
	}
	evidence := toolValue(t, responses[5])
	shots := evidence["screenshots"].([]any)
	if len(shots) != 1 || shots[0].(map[string]any)["url"] != api.URL+"/screenshots/run-1/final%20image.png" {
		t.Fatalf("evidence = %#v", evidence)
	}
	if encoded, _ := json.Marshal(evidence); strings.Contains(string(encoded), "PRIVATE PAGE") || strings.Contains(string(encoded), "evil.example") {
		t.Fatalf("unsafe evidence = %s", encoded)
	}
	if text := responses[1]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string); !json.Valid([]byte(text)) {
		t.Fatalf("tool text is not JSON: %q", text)
	}
	if got := toolValue(t, responses[3]); !reflect.DeepEqual(got, map[string]any{"result": []any{map[string]any{"id": "run-2", "status": "passed"}}}) {
		t.Fatalf("list structured content = %#v", got)
	}
}

func TestToolAPIErrorsAreResults(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"Run not found"}`))
	}))
	defer api.Close()
	server, err := New(api.URL, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	responses := exchange(t, server, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_test_run","arguments":{"run_id":"missing"}}}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_test_runs","arguments":{"limit":"bad"}}}`)
	for _, response := range responses[1:] {
		result := response["result"].(map[string]any)
		if result["isError"] != true {
			t.Fatalf("result = %#v", result)
		}
	}
	if text := responses[1]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]; !strings.Contains(text.(string), "run 'missing' was not found") {
		t.Fatalf("error = %v", text)
	}
	for _, response := range responses[1:] {
		if _, ok := response["result"].(map[string]any)["structuredContent"].(map[string]any); !ok {
			t.Fatalf("structured content = %#v", response)
		}
	}
}

func newTestServer(t *testing.T, handler http.Handler) *Server {
	t.Helper()
	api := httptest.NewServer(handler)
	t.Cleanup(api.Close)
	server, err := New(api.URL, api.Client())
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func exchange(t *testing.T, server *Server, messages ...string) []map[string]any {
	t.Helper()
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(strings.Join(messages, "\n")+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if line == "" {
			continue
		}
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("stdout is not protocol JSON %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func toolValue(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	return response["result"].(map[string]any)["structuredContent"].(map[string]any)
}
