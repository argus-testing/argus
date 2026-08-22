package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrDuplicateTool   = errors.New("duplicate tool")
	ErrUnknownProvider = errors.New("unknown provider")
	ErrUnknownTool     = errors.New("unknown tool")
	ErrInvalidResponse = errors.New("invalid provider response")
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
			toolResult := ToolResultPart{CallID: call.CallID, Name: call.Name, Result: result}
			if err := r.sessions.Append(ctx, sessionID, Message{Role: RoleTool, Parts: []MessagePart{{ToolResult: &toolResult}}}); err != nil {
				return err
			}
			if err := emitEvent(emit, ToolResultEvent{Result: toolResult}); err != nil {
				return err
			}
		}
	}
	return &ModelCallLimitError{Limit: r.maxModelCalls}
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
