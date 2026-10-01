package web

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/infra/provider"
)

func TestQuotaGRPCHeaderFailureCannotPublishSnapshot(t *testing.T) {
	for _, scenario := range []struct {
		status, body string
		unauthorized bool
	}{
		{"14", "", false},
		{"14", capturedWeeklyCreditsHex, false},
		{"16", "", true},
		{"invalid-secret-payload", "", false},
	} {
		t.Run(scenario.status+scenario.body[:min(len(scenario.body), 5)], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/grpc-web+proto")
				w.Header().Set("grpc-status", scenario.status)
				w.Header().Set("grpc-message", "private upstream diagnostic")
				body, err := hex.DecodeString(scenario.body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_, _ = w.Write(body)
			}))
			defer server.Close()
			adapter, credential := testMediaAdapter(t, server.URL)
			window, err := adapter.SyncQuotaMode(context.Background(), credential, "weekly")
			if err == nil || window.SyncedAt != nil || window.Total != 0 || window.Remaining != 0 {
				t.Fatal("failed gRPC response became a quota observation")
			}
			if errors.Is(err, provider.ErrUnauthorized) != scenario.unauthorized {
				t.Fatalf("wrong auth classification: %v", err)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
				t.Fatal("raw upstream diagnostic leaked")
			}
		})
	}
}
