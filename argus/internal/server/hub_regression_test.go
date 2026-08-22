package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ace-foundry/argus-testing/argus/internal/domain"
	"github.com/coder/websocket"
)

func TestStalledSubscriberDoesNotBlockCancelAndReplayRecoversEvents(t *testing.T) {
	server, db, _ := newTestServer(t, "")
	run, err := db.CreateRun("https://example.com", "check", domain.RunPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddEvent(run.ID, domain.EventRunQueued, nil); err != nil {
		t.Fatal(err)
	}
	_, unsubscribe := server.hub.subscribe(run.ID)
	defer unsubscribe()

	published := make(chan error, 1)
	go func() {
		for range 300 {
			event, err := db.AddEvent(run.ID, domain.EventBrowserAction, nil)
			if err != nil {
				published <- err
				return
			}
			server.Publish(*event)
		}
		published <- nil
	}()
	select {
	case err := <-published:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled subscriber blocked event publication")
	}

	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	response := request(t, httpServer.Client(), http.MethodPost, httpServer.URL+"/api/runs/"+run.ID+"/cancel", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cancel = %d", response.StatusCode)
	}
	response.Body.Close()
	unsubscribe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws/runs/"+url.PathEscape(run.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for range 302 {
		_, message, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var event domain.RunEvent
		if err := json.Unmarshal(message, &event); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err = conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("close = %v", err)
	}
}
