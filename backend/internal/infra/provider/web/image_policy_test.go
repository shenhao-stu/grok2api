package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	fhttp "github.com/bogdanfinn/fhttp"
	fhttptest "github.com/bogdanfinn/fhttp/httptest"
	"github.com/bogdanfinn/websocket"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
)

func TestImaginePolicyRequiresAllCompletedExplicitlyModerated(t *testing.T) {
	c := newImagineCollector()
	if c.AllModerated(1) || c.AllModerated(0) {
		t.Fatal("empty batch treated as moderated")
	}
	c.Accept(map[string]any{"type": "json", "id": "first", "current_status": "completed", "moderated": true})
	if !c.AllModerated(1) || c.AllModerated(2) {
		t.Fatal("batch completion ignored")
	}
	c.Accept(map[string]any{"type": "image", "id": "unknown", "percentage_complete": 50})
	if c.AllModerated(1) {
		t.Fatal("unknown slot treated as moderated")
	}
	c.Accept(map[string]any{"type": "json", "id": "unknown", "current_status": "completed"})
	if c.AllModerated(2) {
		t.Fatal("absent moderation treated as rejection")
	}
}

func TestImagineSemanticFailureReturnsTypedErrorWithoutAdapterRetry(t *testing.T) {
	for _, scenario := range []struct {
		name, code string
		status     int
		frame      map[string]any
	}{
		{"moderated", "content_policy_violation", http.StatusBadRequest, map[string]any{"type": "json", "id": "blocked", "current_status": "completed", "moderated": true}},
		{"quota_text", "usage_limit_reached", http.StatusTooManyRequests, map[string]any{"type": "error", "message": "You've used up your media generation credits."}},
		{"quota_code", "usage_limit_reached", http.StatusTooManyRequests, map[string]any{"type": "error", "error": map[string]any{"code": "RESOURCE_EXHAUSTED"}}},
		{"imagine_pool_exhausted", "usage_limit_reached", http.StatusTooManyRequests, map[string]any{"type": "error", "err_code": "usage_pool_exhausted", "err_msg": "Usage limit exceeded"}},
		{"imagine_pool_code_only", "usage_limit_reached", http.StatusTooManyRequests, map[string]any{"type": "error", "err_code": "usage_pool_exhausted"}},
	} {
		for _, streaming := range []bool{false, true} {
			t.Run(scenario.name+map[bool]string{false: "JSON", true: "SSE"}[streaming], func(t *testing.T) {
				var handshakes atomic.Int32
				server := fhttptest.NewServer(fhttp.HandlerFunc(func(w fhttp.ResponseWriter, r *fhttp.Request) {
					handshakes.Add(1)
					conn, err := (&websocket.Upgrader{CheckOrigin: func(*fhttp.Request) bool { return true }}).Upgrade(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					for range 2 {
						var message map[string]any
						if err := conn.ReadJSON(&message); err != nil {
							t.Error(err)
							return
						}
					}
					_ = conn.WriteJSON(scenario.frame)
				}))
				defer server.Close()
				adapter, credential := testMediaAdapter(t, server.URL)
				response, err := adapter.generateWSImage(context.Background(), provider.ImageGenerationRequest{
					Credential: credential, Prompt: "synthetic test", Streaming: streaming,
				}, 1, "b64_json", "1:1", imagineModelConfig{ExpectedCount: 1, MaxReturnCount: 1})
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				want := scenario.status
				if streaming {
					want = http.StatusOK
				}
				if response.StatusCode != want || !strings.Contains(string(body), `"code":"`+scenario.code+`"`) || strings.Contains(string(body), "image_generation.completed") {
					t.Fatalf("status=%d body=%s", response.StatusCode, body)
				}
				if streaming && !strings.Contains(string(body), "event: error") {
					t.Fatalf("missing terminal error: %s", body)
				}
				if handshakes.Load() != 1 {
					t.Fatal("moderation retried upstream")
				}
			})
		}
	}
}

func TestMediaQuotaClassificationDoesNotMatchOrdinaryError(t *testing.T) {
	for _, message := range []string{"unknown server error", "image has no pixels", "content rejected", "media generation credits"} {
		if errors.Is(webResponseError(map[string]any{"message": message}), errWebUsageLimit) {
			t.Fatalf("misclassified: %s", message)
		}
	}
}

func TestImageEditExhaustedCreditsReturnTypedFailure(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "SSE"}[streaming], func(t *testing.T) {
			server := fhttptest.NewServer(fhttp.HandlerFunc(func(w fhttp.ResponseWriter, r *fhttp.Request) {
				switch r.URL.Path {
				case "/http/upload-file-v2/direct":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"uploadId":"fixture","fileMetadata":{"fileMetadataId":"fixture","fileUri":"users/test/reference/content"}}`))
				case "/rest/app-chat/conversations/new":
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: {\"error\":{\"message\":\"You've used up your media generation credits.\"}}\n\n"))
				default:
					fhttp.NotFound(w, r)
				}
			}))
			defer server.Close()
			adapter, credential := testMediaAdapter(t, server.URL)
			response, err := adapter.EditImage(context.Background(), provider.ImageEditRequest{Credential: credential,
				ImageURLs: []string{"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="},
				Prompt:    "synthetic", Count: 1, Resolution: "1k", Streaming: streaming})
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			want := http.StatusTooManyRequests
			if streaming {
				want = http.StatusOK
			}
			if response.StatusCode != want || !strings.Contains(string(body), `"code":"usage_limit_reached"`) || strings.Contains(string(body), "image_edit.completed") {
				t.Fatalf("status=%d body=%s", response.StatusCode, body)
			}
		})
	}
}
