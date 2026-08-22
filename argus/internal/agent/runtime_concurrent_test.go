package agent

import (
	"context"
	goruntime "runtime"
	"sync"
	"testing"
)

type blockingProvider struct {
	mu           sync.Mutex
	requests     []ModelRequest
	firstStarted chan struct{}
	releaseFirst chan struct{}
}

func (p *blockingProvider) Stream(_ context.Context, request ModelRequest, emit func(ModelEvent) error) error {
	p.mu.Lock()
	p.requests = append(p.requests, request)
	call := len(p.requests)
	p.mu.Unlock()
	if call == 1 {
		close(p.firstStarted)
		<-p.releaseFirst
	}
	return emit(ModelResponse{Parts: []ResponsePart{{Text: &TextPart{Text: "done"}}}})
}

func (p *blockingProvider) Requests() []ModelRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ModelRequest(nil), p.requests...)
}

func TestRuntimeSerializesSameSessionRuns(t *testing.T) {
	provider := &blockingProvider{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	runtime := NewRuntime(map[string]Provider{"fake": provider}, NewInMemorySessionStore())
	spec := AgentSpec{Model: ModelRef{Provider: "fake", Model: "model"}}
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- runtime.Run(context.Background(), spec, "session", &Message{Role: RoleUser, Parts: []MessagePart{{Text: &TextPart{Text: "first"}}}}, nil, nil)
	}()
	<-provider.firstStarted

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- runtime.Run(context.Background(), spec, "session", &Message{Role: RoleUser, Parts: []MessagePart{{Text: &TextPart{Text: "second"}}}}, nil, nil)
	}()
	waitForSessionLockRefs(t, runtime, "session", 2)
	close(provider.releaseFirst)

	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	requests := provider.Requests()
	if len(requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(requests))
	}
	if got := requests[1].Messages; len(got) != 3 || got[0].Role != RoleUser || got[1].Role != RoleAssistant || got[2].Role != RoleUser {
		t.Fatalf("second request messages = %#v, want first user, first assistant, second user", got)
	}
}

func waitForSessionLockRefs(t *testing.T, runtime *Runtime, sessionID string, want int) {
	t.Helper()
	for range 10_000 {
		runtime.sessionLocksMu.Lock()
		lock := runtime.sessionLocks[sessionID]
		refs := 0
		if lock != nil {
			refs = lock.refs
		}
		runtime.sessionLocksMu.Unlock()
		if refs >= want {
			return
		}
		goruntime.Gosched()
	}
	t.Fatalf("session lock references did not reach %d", want)
}
