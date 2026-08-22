package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ace-foundry/argus-testing/argus/internal/agent"
)

type scriptedProvider struct {
	responses []agent.ModelResponse
	requests  []agent.ModelRequest
}

func (p *scriptedProvider) Stream(_ context.Context, request agent.ModelRequest, emit func(agent.ModelEvent) error) error {
	p.requests = append(p.requests, request)
	response := p.responses[len(p.requests)-1]
	return emit(response)
}

func TestRuntimeInvokesToolPersistsResultThenCompletes(t *testing.T) {
	t.Parallel()
	provider := &scriptedProvider{responses: []agent.ModelResponse{
		{Parts: []agent.ResponsePart{
			{ToolCall: &agent.ToolCallPart{CallID: "call-1", Name: "lookup", Arguments: map[string]any{"q": "argus"}}},
		}},
		{Parts: []agent.ResponsePart{
			{Text: &agent.TextPart{Text: "done"}},
		}},
	}}
	sessions := agent.NewInMemorySessionStore()
	runtime := agent.NewRuntime(map[string]agent.Provider{"fake": provider}, sessions)
	tool := agent.Tool{
		Name:        "lookup",
		Description: "Look up a value.",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Invoke: func(_ context.Context, arguments map[string]any, _ agent.ToolContext) (any, error) {
			return map[string]any{"value": arguments["q"]}, nil
		},
	}
	var events []agent.RuntimeEvent
	err := runtime.Run(context.Background(), agent.AgentSpec{
		Name: "test", Model: agent.ModelRef{Provider: "fake", Model: "model"}, Instruction: "be useful", Tools: []agent.Tool{tool},
	}, "session", &agent.Message{Role: agent.RoleUser, Parts: []agent.MessagePart{{Text: &agent.TextPart{Text: "go"}}}}, nil, func(event agent.RuntimeEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("model calls = %d, want 2", len(provider.requests))
	}
	if got := provider.requests[1].Messages[2].Parts[0].ToolResult; got == nil || got.CallID != "call-1" || got.Result.(map[string]any)["value"] != "argus" {
		t.Fatalf("second request tool result = %#v", got)
	}
	session, err := sessions.GetOrCreate(context.Background(), "session", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Messages) != 4 { // user, assistant call, tool result, assistant text
		t.Fatalf("persisted messages = %d, want 4", len(session.Messages))
	}
	if _, ok := events[len(events)-1].(agent.CompletedEvent); !ok {
		t.Fatalf("last event = %T, want CompletedEvent", events[len(events)-1])
	}
}

func TestRuntimeFeedsToolScreenshotIntoNextModelTurn(t *testing.T) {
	t.Parallel()
	provider := &scriptedProvider{responses: []agent.ModelResponse{
		{Parts: []agent.ResponsePart{{ToolCall: &agent.ToolCallPart{CallID: "shot-1", Name: "screenshot", Arguments: map[string]any{}}}}},
		{Parts: []agent.ResponsePart{{Text: &agent.TextPart{Text: "observed"}}}},
	}}
	sessions := agent.NewInMemorySessionStore()
	runtime := agent.NewRuntime(map[string]agent.Provider{"fake": provider}, sessions)
	tool := agent.Tool{
		Name: "screenshot", InputSchema: json.RawMessage("{\"type\":\"object\"}"),
		Invoke: func(context.Context, map[string]any, agent.ToolContext) (any, error) {
			return agent.ToolOutput{
				Result: map[string]any{"path": "/shot.png"},
				Followup: []agent.MessagePart{
					{Text: &agent.TextPart{Text: "Fresh browser screenshot."}},
					{Image: &agent.ImagePart{Data: []byte("png"), MediaType: "image/png"}},
				},
			}, nil
		},
	}
	if err := runtime.Run(context.Background(), agent.AgentSpec{
		Model: agent.ModelRef{Provider: "fake"}, Tools: []agent.Tool{tool},
	}, "multimodal", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	second := provider.requests[1].Messages
	if len(second) != 3 || second[1].Role != agent.RoleTool || second[2].Role != agent.RoleUser {
		t.Fatalf("second request messages = %#v", second)
	}
	result := second[1].Parts[0].ToolResult
	if result == nil || !reflect.DeepEqual(result.Result, map[string]any{"path": "/shot.png"}) {
		t.Fatalf("tool result = %#v", result)
	}
	if parts := second[2].Parts; len(parts) != 2 || parts[0].Text == nil || parts[1].Image == nil || string(parts[1].Image.Data) != "png" {
		t.Fatalf("follow-up parts = %#v", parts)
	}
}

func TestRuntimeRejectsExecutableToolFollowupParts(t *testing.T) {
	t.Parallel()
	provider := &scriptedProvider{responses: []agent.ModelResponse{{
		Parts: []agent.ResponsePart{{ToolCall: &agent.ToolCallPart{CallID: "call", Name: "unsafe", Arguments: map[string]any{}}}},
	}}}
	runtime := agent.NewRuntime(map[string]agent.Provider{"fake": provider}, agent.NewInMemorySessionStore())
	tool := agent.Tool{
		Name: "unsafe", InputSchema: json.RawMessage("{}"),
		Invoke: func(context.Context, map[string]any, agent.ToolContext) (any, error) {
			return agent.ToolOutput{Followup: []agent.MessagePart{{ToolCall: &agent.ToolCallPart{Name: "nested"}}}}, nil
		},
	}
	err := runtime.Run(context.Background(), agent.AgentSpec{Model: agent.ModelRef{Provider: "fake"}, Tools: []agent.Tool{tool}}, "unsafe", nil, nil, nil)
	if !errors.Is(err, agent.ErrInvalidToolFollowup) {
		t.Fatalf("error = %v", err)
	}
}

func TestRuntimeRejectsDuplicateTools(t *testing.T) {
	t.Parallel()
	runtime := agent.NewRuntime(map[string]agent.Provider{"fake": &scriptedProvider{}}, agent.NewInMemorySessionStore())
	tool := agent.Tool{Name: "same", InputSchema: json.RawMessage(`{}`), Invoke: func(context.Context, map[string]any, agent.ToolContext) (any, error) { return nil, nil }}
	err := runtime.Run(context.Background(), agent.AgentSpec{Model: agent.ModelRef{Provider: "fake"}, Tools: []agent.Tool{tool, tool}}, "session", nil, nil, nil)
	if !errors.Is(err, agent.ErrDuplicateTool) {
		t.Fatalf("error = %v, want duplicate-tool error", err)
	}
}

func TestRuntimeEnforcesModelCallCap(t *testing.T) {
	t.Parallel()
	call := agent.ResponsePart{ToolCall: &agent.ToolCallPart{CallID: "call", Name: "again", Arguments: map[string]any{}}}
	provider := &scriptedProvider{responses: []agent.ModelResponse{{Parts: []agent.ResponsePart{call}}, {Parts: []agent.ResponsePart{call}}}}
	runtime := agent.NewRuntime(map[string]agent.Provider{"fake": provider}, agent.NewInMemorySessionStore(), agent.WithMaxModelCalls(2))
	tool := agent.Tool{Name: "again", InputSchema: json.RawMessage(`{}`), Invoke: func(context.Context, map[string]any, agent.ToolContext) (any, error) { return nil, nil }}
	err := runtime.Run(context.Background(), agent.AgentSpec{Model: agent.ModelRef{Provider: "fake"}, Tools: []agent.Tool{tool}}, "session", nil, nil, nil)
	var capped *agent.ModelCallLimitError
	if !errors.As(err, &capped) || capped.Limit != 2 {
		t.Fatalf("error = %v, want limit 2", err)
	}
	if !reflect.DeepEqual(len(provider.requests), 2) {
		t.Fatalf("calls = %d, want 2", len(provider.requests))
	}
}
