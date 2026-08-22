package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

var (
	ErrDuplicateTool       = errors.New("duplicate tool")
	ErrUnknownProvider     = errors.New("unknown provider")
	ErrUnknownTool         = errors.New("unknown tool")
	ErrInvalidResponse     = errors.New("invalid provider response")
	ErrInvalidToolFollowup = errors.New("invalid tool follow-up")
)

type ModelCallLimitError struct {
	Limit int
}

func (e *ModelCallLimitError) Error() string {
	return fmt.Sprintf("model call limit exceeded (%d)", e.Limit)
}

type InMemorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{sessions: make(map[string]*Session)}
}

func (s *InMemorySessionStore) GetOrCreate(_ context.Context, id string, state map[string]any) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.sessions[id]; session != nil {
		return session, nil
	}
	session := &Session{ID: id, State: cloneMap(state)}
	s.sessions[id] = session
	return session, nil
}

func (s *InMemorySessionStore) Append(_ context.Context, id string, message Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q does not exist", id)
	}
	session.Messages = append(session.Messages, message)
	return nil
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type RuntimeOption func(*Runtime)

func WithMaxModelCalls(limit int) RuntimeOption {
	return func(runtime *Runtime) {
		runtime.maxModelCalls = limit
	}
}

type Runtime struct {
	providers      map[string]Provider
	sessions       SessionStore
	maxModelCalls  int
	sessionLocksMu sync.Mutex
	sessionLocks   map[string]*sessionLock
}

type sessionLock struct {
	mu   sync.Mutex
	refs int
}

func NewRuntime(providers map[string]Provider, sessions SessionStore, options ...RuntimeOption) *Runtime {
	runtime := &Runtime{providers: providers, sessions: sessions, maxModelCalls: 32, sessionLocks: make(map[string]*sessionLock)}
	for _, option := range options {
		option(runtime)
	}
	return runtime
}

func (r *Runtime) Run(ctx context.Context, spec AgentSpec, sessionID string, message *Message, state map[string]any, emit func(RuntimeEvent) error) error {
	unlock := r.lockSession(sessionID)
	defer unlock()

	tools, err := uniqueTools(spec.Tools)
	if err != nil {
		return err
	}
	provider := r.providers[spec.Model.Provider]
	if provider == nil {
		return fmt.Errorf("%w: %s", ErrUnknownProvider, spec.Model.Provider)
	}
	session, err := r.sessions.GetOrCreate(ctx, sessionID, state)
	if err != nil {
		return err
	}
	if message != nil {
		if err := r.sessions.Append(ctx, sessionID, *message); err != nil {
			return err
		}
	}

	for calls := 0; calls < r.maxModelCalls; calls++ {
		var response *ModelResponse
		request := ModelRequest{
			Model:             spec.Model,
			SystemInstruction: spec.Instruction,
			Messages:          session.Messages,
			Tools:             spec.Tools,
			Generation:        spec.Generation,
		}
		err := provider.Stream(ctx, request, func(event ModelEvent) error {
			switch value := event.(type) {
			case TextDelta:
				return emitEvent(emit, value)
			case *TextDelta:
				if value != nil {
					return emitEvent(emit, *value)
				}
			case ModelResponse:
				copy := value
				response = &copy
			case *ModelResponse:
				response = value
			}
			return nil
		})
		if err != nil {
			return err
		}
		if response == nil {
			return fmt.Errorf("%w: provider did not return a final response", ErrInvalidResponse)
		}

		assistant := Message{Role: RoleAssistant, Parts: response.Parts}
		if err := r.sessions.Append(ctx, sessionID, assistant); err != nil {
			return err
		}
		var toolCalls []ToolCallPart
		for _, part := range response.Parts {
			if part.ToolCall != nil {
				toolCalls = append(toolCalls, *part.ToolCall)
			}
		}
		if len(toolCalls) == 0 {
			return emitEvent(emit, CompletedEvent{Message: assistant})
		}
		for _, call := range toolCalls {
			if err := emitEvent(emit, ToolCallEvent{Call: call}); err != nil {
				return err
			}
			tool := tools[call.Name]
			if tool == nil {
				return fmt.Errorf("%w: %s", ErrUnknownTool, call.Name)
			}
			result, err := tool.Invoke(ctx, call.Arguments, ToolContext{State: session.State})
			if err != nil {
				return err
			}
			result, followup, err := splitToolOutput(result)
			if err != nil {
				return err
			}
			toolResult := ToolResultPart{CallID: call.CallID, Name: call.Name, Result: result}
			if err := r.sessions.Append(ctx, sessionID, Message{Role: RoleTool, Parts: []MessagePart{{ToolResult: &toolResult}}}); err != nil {
				return err
			}
			if err := emitEvent(emit, ToolResultEvent{Result: toolResult}); err != nil {
				return err
			}
			if len(followup) > 0 {
				if err := r.sessions.Append(ctx, sessionID, Message{Role: RoleUser, Parts: followup}); err != nil {
					return err
				}
			}
		}
	}
	return &ModelCallLimitError{Limit: r.maxModelCalls}
}

func splitToolOutput(value any) (any, []MessagePart, error) {
	var output ToolOutput
	switch value := value.(type) {
	case ToolOutput:
		output = value
	case *ToolOutput:
		if value == nil {
			return nil, nil, fmt.Errorf("%w: output is nil", ErrInvalidToolFollowup)
		}
		output = *value
	default:
		return value, nil, nil
	}
	followup, err := validateFollowup(output.Followup)
	if err != nil {
		return nil, nil, err
	}
	return output.Result, followup, nil
}

func validateFollowup(parts []MessagePart) ([]MessagePart, error) {
	if len(parts) > 8 {
		return nil, fmt.Errorf("%w: too many parts", ErrInvalidToolFollowup)
	}
	validated := make([]MessagePart, len(parts))
	for index, part := range parts {
		kinds := 0
		if part.Text != nil {
			kinds++
		}
		if part.Image != nil {
			kinds++
		}
		if part.Audio != nil {
			kinds++
		}
		if part.ToolCall != nil || part.ToolResult != nil {
			return nil, fmt.Errorf("%w: executable parts are not allowed", ErrInvalidToolFollowup)
		}
		if kinds != 1 {
			return nil, fmt.Errorf("%w: each part must contain exactly one observation", ErrInvalidToolFollowup)
		}
		switch {
		case part.Text != nil:
			if !utf8.ValidString(part.Text.Text) || utf8.RuneCountInString(part.Text.Text) > 100_000 {
				return nil, fmt.Errorf("%w: text is invalid", ErrInvalidToolFollowup)
			}
			validated[index].Text = &TextPart{Text: part.Text.Text}
		case part.Image != nil:
			if len(part.Image.Data) == 0 || len(part.Image.Data) > 10<<20 || !strings.HasPrefix(part.Image.MediaType, "image/") {
				return nil, fmt.Errorf("%w: image is invalid", ErrInvalidToolFollowup)
			}
			validated[index].Image = &ImagePart{Data: append([]byte(nil), part.Image.Data...), MediaType: part.Image.MediaType}
		case part.Audio != nil:
			if len(part.Audio.Data) == 0 || len(part.Audio.Data) > 10<<20 || !strings.HasPrefix(part.Audio.MediaType, "audio/") {
				return nil, fmt.Errorf("%w: audio is invalid", ErrInvalidToolFollowup)
			}
			validated[index].Audio = &AudioPart{Data: append([]byte(nil), part.Audio.Data...), MediaType: part.Audio.MediaType}
		}
	}
	return validated, nil
}

func (r *Runtime) lockSession(id string) func() {
	r.sessionLocksMu.Lock()
	lock := r.sessionLocks[id]
	if lock == nil {
		lock = &sessionLock{}
		r.sessionLocks[id] = lock
	}
	lock.refs++
	r.sessionLocksMu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		r.sessionLocksMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(r.sessionLocks, id)
		}
		r.sessionLocksMu.Unlock()
	}
}

func uniqueTools(tools []Tool) (map[string]*Tool, error) {
	byName := make(map[string]*Tool, len(tools))
	for index := range tools {
		tool := &tools[index]
		if _, exists := byName[tool.Name]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateTool, tool.Name)
		}
		byName[tool.Name] = tool
	}
	return byName, nil
}

func emitEvent(emit func(RuntimeEvent) error, event RuntimeEvent) error {
	if emit == nil {
		return nil
	}
	return emit(event)
}
