package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
)

func TestXMLToolActualAdapterBoundary(t *testing.T) {
	for _, model := range []string{"grok-4.6", "grok-4.7", "grok-4.7-build-fast"} {
		for kind, marker := range map[string]string{"chat": "tool_calls", "messages": "tool_use", "responses": "function_call"} {
			for _, stream := range []bool{false, true} {
				t.Run(model+"/"+kind+map[bool]string{false: "_json", true: "_sse"}[stream], func(t *testing.T) {
					c := xmlTestCompatibility(t, `"auto"`)
					function := map[string]any{"name": "save_note", "parameters": c.functionSchemas["save_note"]}
					body := map[string]any{"model": model, "stream": stream, "max_tokens": 100, "messages": []any{map[string]any{"role": "user", "content": "Call save_note"}}}
					switch kind {
					case "chat":
						body["tools"] = []any{map[string]any{"type": "function", "function": function}}
					case "messages":
						body["tools"] = []any{map[string]any{"name": "save_note", "input_schema": function["parameters"]}}
					case "responses":
						delete(body, "messages")
						delete(body, "max_tokens")
						body["input"] = "Call save_note"
						body["tools"] = c.visibleTools
					}
					request, _ := json.Marshal(body)
					response := map[string]any{"id": "resp_test", "model": model, "status": "completed", "output": []any{xmlTestMessage(xmlToolFixture)}, "usage": map[string]any{"input_tokens": 20, "output_tokens": 30}}
					encoded, _ := json.Marshal(response)
					adapter, encrypted := newFallbackTestAdapter(t)
					adapter.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
						var wireBody map[string]any
						if err := json.NewDecoder(r.Body).Decode(&wireBody); err != nil || wireBody["model"] != model {
							t.Fatalf("model identity changed before upstream: %v", wireBody["model"])
						}
						res := jsonResponse(200, string(encoded), r)
						if stream {
							var wire strings.Builder
							emit := func(p map[string]any) {
								data, _ := json.Marshal(p)
								wire.WriteString("event: " + p["type"].(string) + "\ndata: " + string(data) + "\n\n")
							}
							emit(map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_test", "model": model, "status": "in_progress", "output": []any{}}})
							emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": xmlTestMessage("")})
							for _, part := range []string{xmlToolFixture[:3], xmlToolFixture[3:]} {
								emit(map[string]any{"type": "response.output_text.delta", "item_id": "msg_test", "output_index": 0, "content_index": 0, "delta": part})
							}
							emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": xmlTestMessage(xmlToolFixture)})
							emit(map[string]any{"type": "response.completed", "response": response})
							wire.WriteString("data: [DONE]\n\n")
							res.Body = io.NopCloser(strings.NewReader(wire.String()))
							res.Header.Set("Content-Type", "text/event-stream")
						}
						return res, nil
					})
					res, err := adapter.ForwardResponse(context.Background(), provider.ResponseResourceRequest{Credential: account.Credential{ID: 7, Provider: account.ProviderBuild, EncryptedAccessToken: encrypted}, Method: http.MethodPost, Path: "/responses", Model: model, Operation: kind, NormalizeBody: true, Streaming: stream, Body: request})
					if err != nil {
						t.Fatal(err)
					}
					defer res.Body.Close()
					data, err := io.ReadAll(res.Body)
					if err != nil || res.StatusCode != 200 || !strings.Contains(string(data), marker) || strings.Contains(string(data), "\\u003ctool") || strings.Contains(string(data), "<tool_call>") {
						t.Fatalf("normalization skipped: %s %v", data, err)
					}
				})
			}
		}
	}
}
