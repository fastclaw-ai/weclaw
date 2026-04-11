package agent

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}

func TestACPAgentResetSessionFallsBackToCodexThreads(t *testing.T) {
	ctx := context.Background()
	ag := NewACPAgent(ACPAgentConfig{
		Command: "/usr/local/bin/codex",
	})
	ag.started = true
	ag.stdin = nopWriteCloser{Writer: io.Discard}

	var methods []string
	ag.rpcCall = func(_ context.Context, method string, _ interface{}) (json.RawMessage, error) {
		methods = append(methods, method)

		switch method {
		case "session/new":
			return nil, assertCodexMethodError("session/new")
		case "thread/start":
			return json.RawMessage(`{"thread":{"id":"thread-123"}}`), nil
		default:
			t.Fatalf("unexpected method: %s", method)
			return nil, nil
		}
	}

	threadID, err := ag.ResetSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("ResetSession() error = %v", err)
	}
	if threadID != "thread-123" {
		t.Fatalf("ResetSession() threadID = %q, want %q", threadID, "thread-123")
	}
	if ag.protocol != protocolCodexAppServer {
		t.Fatalf("protocol = %q, want %q", ag.protocol, protocolCodexAppServer)
	}
	if !ag.codexInitialized {
		t.Fatal("codexInitialized = false, want true")
	}

	wantMethods := []string{"session/new", "thread/start"}
	if len(methods) != len(wantMethods) {
		t.Fatalf("methods = %v, want %v", methods, wantMethods)
	}
	for i := range wantMethods {
		if methods[i] != wantMethods[i] {
			t.Fatalf("methods[%d] = %q, want %q", i, methods[i], wantMethods[i])
		}
	}
}

func TestACPAgentChatFallsBackToCodexThreads(t *testing.T) {
	ctx := context.Background()
	ag := NewACPAgent(ACPAgentConfig{
		Command: "/usr/local/bin/codex",
	})
	ag.started = true
	ag.stdin = nopWriteCloser{Writer: io.Discard}

	var methods []string
	ag.rpcCall = func(_ context.Context, method string, _ interface{}) (json.RawMessage, error) {
		methods = append(methods, method)

		switch method {
		case "session/new":
			return nil, assertCodexMethodError("session/new")
		case "thread/start":
			return json.RawMessage(`{"thread":{"id":"thread-456"}}`), nil
		case "turn/start":
			go func() {
				ag.dispatchToTurnCh("thread-456", &codexTurnEvent{Delta: "hello from codex"})
				ag.dispatchToTurnCh("thread-456", &codexTurnEvent{Kind: "completed"})
			}()
			return json.RawMessage(`{"turn":{"id":"turn-1"}}`), nil
		default:
			t.Fatalf("unexpected method: %s", method)
			return nil, nil
		}
	}

	reply, err := ag.Chat(ctx, "user-1", "hi")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if reply != "hello from codex" {
		t.Fatalf("Chat() reply = %q, want %q", reply, "hello from codex")
	}
	if ag.protocol != protocolCodexAppServer {
		t.Fatalf("protocol = %q, want %q", ag.protocol, protocolCodexAppServer)
	}

	wantMethods := []string{"session/new", "thread/start", "turn/start"}
	if len(methods) != len(wantMethods) {
		t.Fatalf("methods = %v, want %v", methods, wantMethods)
	}
	for i := range wantMethods {
		if methods[i] != wantMethods[i] {
			t.Fatalf("methods[%d] = %q, want %q", i, methods[i], wantMethods[i])
		}
	}
}

func assertCodexMethodError(method string) error {
	return &rpcError{
		Message: "Invalid request: unknown variant " + method + ", expected one of initialize, thread/start, thread/resume, turn/start",
	}
}

func (e *rpcError) Error() string {
	return "agent error: " + e.Message
}
