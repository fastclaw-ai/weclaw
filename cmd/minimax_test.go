package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/fastclaw-ai/weclaw/config"
)

type minimaxTransport func(*http.Request) (*http.Response, error)

func (f minimaxTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// Exercise detection, saved configuration, the startup factory and the actual
// serialized HTTP request without contacting a provider or launching agents.
func TestMinimaxPresetHTTPRequest(t *testing.T) {
	const global = "https://api.minimax.io/v1/chat/completions"
	const china = "https://api.minimaxi.com/v1/chat/completions"
	const custom = "https://proxy.example.com/v1/chat/completions"
	for _, tc := range []struct {
		name, region, model, endpoint, wantModel, wantEndpoint string
		savedAgent                                             string
		saved                                                  bool
	}{
		{name: "global default", wantModel: "MiniMax-M3.1-Flash", wantEndpoint: global},
		{name: "China default", region: "cn_zh", wantModel: "MiniMax-M3.1-Flash", wantEndpoint: china},
		{name: "unknown region", region: "unknown", wantModel: "MiniMax-M3.1-Flash", wantEndpoint: global},
		{name: "M3", model: "MiniMax-M3", wantModel: "MiniMax-M3", wantEndpoint: global},
		{name: "China M3", region: "cn_zh", model: "MiniMax-M3", wantModel: "MiniMax-M3", wantEndpoint: china},
		{name: "explicit legacy", model: "MiniMax-M2.7", wantModel: "MiniMax-M2.7", wantEndpoint: global},
		{name: "custom route", model: "custom-model", endpoint: custom + "/", wantModel: "custom-model", wantEndpoint: custom},
		{name: "saved M3", saved: true, model: "MiniMax-M3", wantModel: "MiniMax-M3", wantEndpoint: custom},
		{name: "saved legacy", saved: true, model: "MiniMax-M2.7", wantModel: "MiniMax-M2.7", wantEndpoint: custom},
		{name: "saved custom", saved: true, model: "custom-model", wantModel: "custom-model", wantEndpoint: custom},
		{name: "saved non-MiniMax default", saved: true, savedAgent: "custom-http", model: "custom-model", wantModel: "custom-model", wantEndpoint: custom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PATH", t.TempDir())
			t.Setenv("OPENCLAW_GATEWAY_URL", "")
			t.Setenv("WECLAW_DEFAULT_AGENT", "")
			t.Setenv("MINIMAX_API_KEY", "test-key")
			t.Setenv("MINIMAX_REGION", tc.region)
			t.Setenv("MINIMAX_MODEL", tc.model)
			t.Setenv("MINIMAX_BASE_URL", tc.endpoint)

			cfg := config.DefaultConfig()
			agentName := "minimax"
			if tc.savedAgent != "" {
				agentName = tc.savedAgent
			}
			var saved config.AgentConfig
			if tc.saved {
				saved = config.AgentConfig{
					Type: "http", Endpoint: custom, APIKey: "saved-test-key", Model: tc.model,
					Headers:      map[string]string{"X-Custom": "preserved"},
					SystemPrompt: "Saved instructions", MaxHistory: 4,
					Aliases: []string{"saved-alias"},
				}
				cfg.Agents[agentName] = saved
				cfg.DefaultAgent = agentName
				if err := config.Save(cfg); err != nil {
					t.Fatal(err)
				}
				var err error
				cfg, err = config.Load()
				if err != nil {
					t.Fatal(err)
				}
				// New environment defaults must not migrate saved agent settings.
				t.Setenv("MINIMAX_MODEL", "MiniMax-M3.1-Flash")
			}
			config.DetectAndConfigure(cfg)
			if cfg.DefaultAgent != agentName {
				t.Fatalf("default agent = %q, want %q", cfg.DefaultAgent, agentName)
			}
			if tc.saved && !reflect.DeepEqual(cfg.Agents[agentName], saved) {
				t.Fatalf("saved configuration changed: %#v", cfg.Agents[agentName])
			}
			if tc.savedAgent != "" && cfg.Agents["minimax"].Model != "MiniMax-M3.1-Flash" {
				t.Fatal("expected the MiniMax preset alongside the saved default agent")
			}

			var requests []map[string]any
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = minimaxTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPost || r.URL.String() != tc.wantEndpoint {
					t.Errorf("request = %s %s, want POST %s", r.Method, r.URL, tc.wantEndpoint)
				}
				wantKey := "test-key"
				if tc.saved {
					wantKey = "saved-test-key"
					if r.Header.Get("X-Custom") != "preserved" {
						t.Error("saved custom header was lost")
					}
				}
				if r.Header.Get("Authorization") != "Bearer "+wantKey || r.Header.Get("Content-Type") != "application/json" {
					t.Error("unexpected authentication or content type")
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				requests = append(requests, payload)
				if payload["model"] != tc.wantModel {
					t.Errorf("model = %v, want %s", payload["model"], tc.wantModel)
				}
				// The preset leaves thinking/effort at the API defaults. In
				// particular it must not disable Flash thinking or inject tiers.
				for _, field := range []string{"thinking", "reasoning_effort", "reasoning_split", "service_tier"} {
					if _, exists := payload[field]; exists {
						t.Errorf("unexpected request field %q", field)
					}
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"reply"}}]}`)),
				}, nil
			})

			ag := createAgentByName(context.Background(), cfg, cfg.DefaultAgent)
			if ag == nil {
				t.Fatal("startup did not create the HTTP agent")
			}
			for _, prompt := range []string{"first", "second"} {
				reply, err := ag.Chat(context.Background(), "conversation", prompt)
				if err != nil || reply != "reply" {
					t.Fatalf("Chat = %q, %v", reply, err)
				}
			}
			if len(requests) != 2 {
				t.Fatalf("request count = %d, want 2", len(requests))
			}
			wantMessages := []any{
				map[string]any{"role": "user", "content": "first"},
				map[string]any{"role": "assistant", "content": "reply"},
				map[string]any{"role": "user", "content": "second"},
			}
			if tc.saved {
				wantMessages = append([]any{map[string]any{"role": "system", "content": "Saved instructions"}}, wantMessages...)
			}
			if !reflect.DeepEqual(requests[1]["messages"], wantMessages) {
				t.Fatalf("conversation history = %#v, want %#v", requests[1]["messages"], wantMessages)
			}
		})
	}
}
