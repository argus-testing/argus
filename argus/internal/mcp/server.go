// Package mcp implements Argus's local stdio MCP adapter.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ace-foundry/argus-testing/argus/internal/domain"
)

const (
	DefaultBaseURL  = "http://127.0.0.1:8000"
	protocolVersion = "2025-11-25"
	maxReportText   = 4_000
	maxDetailText   = 2_000
	maxReportItems  = 20
	maxEventTypes   = 20
	maxScreenshots  = 50
)

var supportedProtocolVersions = map[string]bool{"2024-11-05": true, "2025-03-26": true, "2025-06-18": true, protocolVersion: true}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type Server struct {
	client      *Client
	context     context.Context
	initialized bool
}

func New(baseURL string, httpClient *http.Client) (*Server, error) {
	client, err := NewClient(baseURL, httpClient)
	if err != nil {
		return nil, err
	}
	return &Server{client: client, context: context.Background()}, nil
}

func NewFromEnv() (*Server, error) {
	baseURL := os.Getenv("ARGUS_BASE_URL")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return New(baseURL, nil)
}

func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("ARGUS_BASE_URL must be an HTTP(S) URL without credentials")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	client := *httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: strings.TrimRight(parsed.String(), "/"), httpClient: &client}, nil
}

// Serve reads newline-delimited JSON-RPC messages and writes only JSON-RPC messages.
func (s *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	s.context = ctx
	messages := make(chan []byte)
	go func() {
		defer close(messages)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4*1024), 4<<20)
		for scanner.Scan() {
			message := append([]byte(nil), scanner.Bytes()...)
			select {
			case messages <- message:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case messages <- nil:
			case <-ctx.Done():
			}
		}
	}()
	encoder := json.NewEncoder(output)
	for {
		select {
		case <-ctx.Done():
			return nil
		case message, ok := <-messages:
			if !ok {
				return nil
			}
			response := s.handle(message)
			if response != nil {
				if err := encoder.Encode(response); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) handle(raw []byte) any {
	if len(raw) == 0 || !json.Valid(raw) {
		return rpcError(nil, -32700, "Parse error")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return rpcError(nil, -32600, "Invalid Request")
	}
	var version, method string
	if !decodeField(fields, "jsonrpc", &version) || version != "2.0" || !decodeField(fields, "method", &method) || method == "" {
		return rpcError(nil, -32600, "Invalid Request")
	}
	id, hasID, validID := requestID(fields)
	if !validID {
		return rpcError(nil, -32600, "Invalid Request")
	}
	result, code, message := s.dispatch(method, fields["params"])
	if !hasID {
		return nil
	}
	if code != 0 {
		return rpcError(id, code, message)
	}
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func (s *Server) dispatch(method string, rawParams json.RawMessage) (any, int, string) {
	if method == "notifications/initialized" {
		return nil, 0, ""
	}
	if method == "initialize" {
		params, err := objectParams(rawParams)
		if err != nil {
			return nil, -32602, "Invalid params"
		}
		version, ok := params["protocolVersion"].(string)
		_, capabilitiesOK := params["capabilities"].(map[string]any)
		clientInfo, clientInfoOK := params["clientInfo"].(map[string]any)
		name, nameOK := clientInfo["name"].(string)
		clientVersion, clientVersionOK := clientInfo["version"].(string)
		if !ok || !capabilitiesOK || !clientInfoOK || !nameOK || name == "" || !clientVersionOK || clientVersion == "" {
			return nil, -32602, "Invalid params"
		}
		if !supportedProtocolVersions[version] {
			version = protocolVersion
		}
		s.initialized = true
		return map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "Argus", "version": "0.1.0"}, "instructions": "Start and inspect test runs on a local Argus server."}, 0, ""
	}
	if method == "ping" {
		return map[string]any{}, 0, ""
	}
	if !s.initialized {
		return nil, -32002, "Server not initialized"
	}
	switch method {
	case "tools/list":
		return map[string]any{"tools": tools()}, 0, ""
	case "tools/call":
		params, err := objectParams(rawParams)
		if err != nil {
			return nil, -32602, "Invalid params"
		}
		name, ok := params["name"].(string)
		if !ok {
			return nil, -32602, "Invalid params"
		}
		arguments := map[string]any{}
		if value, exists := params["arguments"]; exists {
			arguments, ok = value.(map[string]any)
			if !ok {
				return nil, -32602, "Invalid params"
			}
		}
		return toolResult(s.callTool(name, arguments)), 0, ""
	default:
		return nil, -32601, "Method not found"
	}
}

func (s *Server) callTool(name string, arguments map[string]any) (any, error) {
	switch name {
	case "start_test":
		value, err := requiredString(arguments, "url")
		if err != nil {
			return nil, err
		}
		instructions, err := requiredString(arguments, "instructions")
		if err != nil {
			return nil, err
		}
		rawRun, err := s.request(http.MethodPost, domain.RunsEndpoint, "", map[string]string{"url": value, "instructions": instructions})
		if err != nil {
			return nil, err
		}
		run, err := runResponse(rawRun)
		if err != nil {
			return nil, err
		}
		id := run["id"].(string)
		return map[string]any{"run_id": id, "status": run["status"], "polling_hint": fmt.Sprintf("Poll get_test_run with run_id '%s' for updates.", id)}, nil
	case "get_test_run":
		id, err := requiredString(arguments, "run_id")
		if err != nil {
			return nil, err
		}
		return s.getRun(id)
	case "list_test_runs":
		limit := int64(20)
		if value, exists := arguments["limit"]; exists {
			number, ok := value.(json.Number)
			if !ok {
				return nil, errors.New("Input validation error: limit must be an integer")
			}
			parsed, err := number.Int64()
			if err != nil {
				return nil, errors.New("Input validation error: limit must be an integer")
			}
			limit = parsed
		}
		data, err := s.request(http.MethodGet, domain.RunsEndpoint+"?limit="+strconv.FormatInt(limit, 10), "", nil)
		if err != nil {
			return nil, err
		}
		list, ok := data.([]any)
		if !ok {
			return nil, errors.New("Argus returned an invalid run list response")
		}
		result := make([]any, 0, len(list))
		for _, value := range list {
			run, err := runResponse(value)
			if err != nil {
				return nil, err
			}
			result = append(result, run)
		}
		return result, nil
	case "cancel_test":
		id, err := requiredString(arguments, "run_id")
		if err != nil {
			return nil, err
		}
		data, err := s.request(http.MethodPost, runPath(id)+"/cancel", id, nil)
		if err != nil {
			return nil, err
		}
		return runResponse(data)
	case "get_test_evidence":
		id, err := requiredString(arguments, "run_id")
		if err != nil {
			return nil, err
		}
		run, err := s.getRun(id)
		if err != nil {
			return nil, err
		}
		return s.evidence(id, run.(map[string]any))
	default:
		return nil, fmt.Errorf("Unknown tool: %s", name)
	}
}

func (s *Server) getRun(id string) (any, error) {
	data, err := s.request(http.MethodGet, runPath(id), id, nil)
	if err != nil {
		return nil, err
	}
	return runResponse(data)
}

func (s *Server) evidence(id string, run map[string]any) (any, error) {
	events, ok := run["events"].([]any)
	if !ok {
		return nil, errors.New("Argus returned an invalid evidence response: events missing")
	}
	counts := map[string]int{}
	screenshots := make([]any, 0)
	for _, value := range events {
		event, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("Argus returned an invalid evidence event")
		}
		typ, ok := event["type"].(string)
		if !ok || utf8.RuneCountInString(typ) > 100 {
			return nil, errors.New("Argus returned an invalid evidence event type")
		}
		counts[typ]++
		if typ == "browser.screenshot" && len(screenshots) < maxScreenshots {
			if screenshot := s.screenshot(id, event); screenshot != nil {
				screenshots = append(screenshots, screenshot)
			}
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	byType := map[string]any{}
	for _, key := range keys[:min(len(keys), maxEventTypes)] {
		byType[key] = counts[key]
	}
	summary := map[string]any{"total": len(events), "by_type": byType}
	if len(keys) > maxEventTypes {
		summary["omitted_types"] = len(keys) - maxEventTypes
	}
	report, err := boundedReport(run["report"])
	if err != nil {
		return nil, err
	}
	return map[string]any{"run_id": run["id"], "status": run["status"], "report": report, "event_summary": summary, "screenshots": screenshots}, nil
}

func (s *Server) screenshot(runID string, event map[string]any) map[string]string {
	data, ok := event["data"].(map[string]any)
	if !ok {
		return nil
	}
	path, ok := data["path"].(string)
	if !ok || utf8.RuneCountInString(path) > 2_000 {
		return nil
	}
	parsed, err := url.Parse(path)
	prefix := "/screenshots/" + runID + "/"
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasPrefix(parsed.Path, prefix) {
		return nil
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == ".." {
			return nil
		}
	}
	result := map[string]string{"url": s.client.baseURL + quoteScreenshotPath(parsed.Path)}
	if label, ok := data["label"].(string); ok {
		result["label"] = truncate(label, 200)
	}
	if createdAt, ok := event["created_at"].(string); ok {
		result["created_at"] = truncate(createdAt, 100)
	}
	ordered := map[string]string{}
	for _, key := range []string{"label", "created_at", "url"} {
		if value, ok := result[key]; ok {
			ordered[key] = value
		}
	}
	return ordered
}

func (s *Server) request(method, path, runID string, body any) (any, error) {
	return s.client.request(s.context, method, path, runID, body)
}

func (c *Client) request(parent context.Context, method, path, runID string, body any) (any, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("Argus server unavailable at %s: %w", c.baseURL, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Argus server unavailable at %s: %w", c.baseURL, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("Argus server unavailable at %s: %w", c.baseURL, err)
	}
	if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
		return nil, fmt.Errorf("Argus API error %d: %s", response.StatusCode, apiErrorDetail(data, response.StatusCode))
	}
	if response.StatusCode == http.StatusNotFound {
		if runID != "" {
			return nil, fmt.Errorf("Argus run '%s' was not found", runID)
		}
		return nil, fmt.Errorf("Argus API endpoint '%s' was not found at %s; check ARGUS_BASE_URL and server compatibility", strings.Split(path, "?")[0], c.baseURL)
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("Argus API error %d: %s", response.StatusCode, apiErrorDetail(data, response.StatusCode))
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, errors.New("Argus returned invalid JSON")
	}
	return value, nil
}

func tools() []any {
	return []any{
		tool("start_test", "Start an Argus UI test and return its durable run ID immediately.", map[string]any{"url": stringSchema("Url"), "instructions": stringSchema("Instructions")}, []string{"url", "instructions"}, "start_testArguments", dictOutput("start_testDictOutput")),
		tool("get_test_run", "Get the current canonical state of one Argus test run.", map[string]any{"run_id": stringSchema("Run Id")}, []string{"run_id"}, "get_test_runArguments", dictOutput("get_test_runDictOutput")),
		tool("list_test_runs", "List recent Argus test runs, newest first.", map[string]any{"limit": map[string]any{"default": 20, "title": "Limit", "type": "integer"}}, nil, "list_test_runsArguments", map[string]any{"properties": map[string]any{"result": map[string]any{"items": map[string]any{"additionalProperties": true, "type": "object"}, "title": "Result", "type": "array"}}, "required": []string{"result"}, "title": "list_test_runsOutput", "type": "object"}),
		tool("cancel_test", "Cancel a queued or running Argus test.", map[string]any{"run_id": stringSchema("Run Id")}, []string{"run_id"}, "cancel_testArguments", dictOutput("cancel_testDictOutput")),
		tool("get_test_evidence", "Get a bounded report, event summary, and screenshot URLs for a run.", map[string]any{"run_id": stringSchema("Run Id")}, []string{"run_id"}, "get_test_evidenceArguments", dictOutput("get_test_evidenceDictOutput")),
	}
}

func tool(name, description string, properties map[string]any, required []string, title string, outputSchema map[string]any) map[string]any {
	schema := map[string]any{"properties": properties, "title": title, "type": "object"}
	if len(required) > 0 {
		schema["required"] = required
	}
	return map[string]any{"name": name, "description": description, "inputSchema": schema, "outputSchema": outputSchema}
}
func dictOutput(title string) map[string]any {
	return map[string]any{"additionalProperties": true, "title": title, "type": "object"}
}
func stringSchema(title string) map[string]any {
	return map[string]any{"title": title, "type": "string"}
}
func toolResult(value any, err error) map[string]any {
	if err != nil {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "structuredContent": map[string]any{}, "isError": true}
	}
	structured, ok := value.(map[string]any)
	if !ok {
		structured = map[string]any{"result": value}
	}
	text, _ := json.MarshalIndent(value, "", "  ")
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}, "structuredContent": structured, "isError": false}
}
func runPath(id string) string { return domain.RunsEndpoint + "/" + escapeSegment(id) }
func runResponse(value any) (map[string]any, error) {
	run, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("Argus returned an invalid run response")
	}
	id, idOK := run["id"].(string)
	status, statusOK := run["status"].(string)
	if !idOK || !statusOK || utf8.RuneCountInString(id) > 128 || utf8.RuneCountInString(status) > 100 {
		return nil, errors.New("Argus returned an invalid run response")
	}
	return run, nil
}
func boundedReport(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	report, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("Argus returned an invalid evidence response: report malformed")
	}
	bounded := map[string]any{}
	for _, field := range []struct {
		name  string
		limit int
	}{{"verdict", 100}, {"summary", maxReportText}, {"plan", maxReportText}} {
		if text, ok := report[field.name].(string); ok {
			bounded[field.name] = truncate(text, field.limit)
		}
	}
	if findings, ok := report["findings"].([]any); ok {
		output := make([]any, 0, min(len(findings), maxReportItems))
		for _, value := range findings[:min(len(findings), maxReportItems)] {
			if finding, ok := value.(map[string]any); ok {
				item := map[string]any{}
				for _, field := range []string{"severity", "title", "detail"} {
					if text, ok := finding[field].(string); ok {
						item[field] = truncate(text, maxDetailText)
					}
				}
				output = append(output, item)
			}
		}
		bounded["findings"] = output
	}
	if recommendations, ok := report["recommendations"].([]any); ok {
		output := make([]any, 0, min(len(recommendations), maxReportItems))
		for _, value := range recommendations[:min(len(recommendations), maxReportItems)] {
			if text, ok := value.(string); ok {
				output = append(output, truncate(text, maxDetailText))
			}
		}
		bounded["recommendations"] = output
	}
	return bounded, nil
}
func apiErrorDetail(body []byte, status int) string {
	var value any
	if json.Unmarshal(body, &value) == nil {
		if object, ok := value.(map[string]any); ok {
			if detail, ok := object["detail"].(string); ok {
				return truncate(detail, 500)
			}
		}
		return "request failed"
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = http.StatusText(status)
	}
	return truncate(text, 500)
}
func truncate(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit]) + "…"
}
func requiredString(values map[string]any, name string) (string, error) {
	value, ok := values[name].(string)
	if !ok {
		return "", fmt.Errorf("Input validation error: %s must be a string", name)
	}
	return value, nil
}
func objectParams(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var values map[string]any
	if err := decoder.Decode(&values); err != nil || values == nil {
		return nil, errors.New("invalid params")
	}
	return values, nil
}
func decodeField(fields map[string]json.RawMessage, name string, target any) bool {
	raw, ok := fields[name]
	return ok && json.Unmarshal(raw, target) == nil
}
func requestID(fields map[string]json.RawMessage) (any, bool, bool) {
	raw, exists := fields["id"]
	if !exists {
		return nil, false, true
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return nil, true, false
	}
	switch value.(type) {
	case nil, string, json.Number:
		return value, true, true
	default:
		return nil, true, false
	}
}
func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}
func escapeSegment(value string) string {
	return percentEncode(value, func(char byte) bool {
		return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.' || char == '~'
	})
}
func quoteScreenshotPath(value string) string {
	return percentEncode(value, func(char byte) bool {
		return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("/%:@-._~", rune(char))
	})
}
func percentEncode(value string, safe func(byte) bool) string {
	var result strings.Builder
	for _, char := range []byte(value) {
		if safe(char) {
			result.WriteByte(char)
		} else {
			fmt.Fprintf(&result, "%%%02X", char)
		}
	}
	return result.String()
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
