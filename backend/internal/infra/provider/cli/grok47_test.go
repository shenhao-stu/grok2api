package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
)

func TestGrok47NormalizationKeepsXHighAndEncryptedReasoning(t *testing.T) {
	for _, model := range []string{"grok-4.7", modeldomain.Grok47BuildFast} {
		body, _, err := normalizeResponsesRequest([]byte(`{"input":"hello","reasoning":{"effort":"xhigh"}}`), model)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Model     string `json:"model"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
			Include []string `json:"include"`
		}
		if json.Unmarshal(body, &payload) != nil || payload.Model != model || payload.Reasoning.Effort != "xhigh" || !slices.Contains(payload.Include, "reasoning.encrypted_content") {
			t.Fatalf("4.7 contract lost: %s", body)
		}
	}
}

func TestGrok47FastRequiresDiscoveredPaidCapability(t *testing.T) {
	a := &Adapter{}
	credential := account.Credential{Provider: account.ProviderBuild, AuthType: account.AuthTypeOAuth}
	for _, tc := range []struct {
		name    string
		billing *account.Billing
		paid    bool
	}{
		{"unknown", nil, false}, {"free", &account.Billing{PlanName: "free"}, false},
		{"super", &account.Billing{PlanName: "SuperGrok"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := a.NormalizeAccountModelCapabilities([]string{"grok-4.7", modeldomain.Grok47BuildFast}, tc.billing, credential)
			if slices.Contains(got, modeldomain.Grok47BuildFast) != tc.paid || !slices.Contains(got, "grok-4.7") {
				t.Fatalf("entitlement mismatch: %v", got)
			}
			if got := a.NormalizeAccountModelCapabilities([]string{"grok-4.7"}, tc.billing, credential); slices.Contains(got, modeldomain.Grok47BuildFast) {
				t.Fatal("Fast must never be invented from standard Grok 4.7")
			}
		})
	}
}

func TestGrok47FastNeverUsesPublicAPIOrDowngradesAfter403(t *testing.T) {
	for _, mode := range []account.BuildRouteMode{account.BuildRouteAuto, account.BuildRouteXAI, account.BuildRouteBuild} {
		t.Run(string(mode), func(t *testing.T) {
			a, encrypted := newFallbackTestAdapter(t)
			calls := 0
			a.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if !strings.HasPrefix(r.URL.String(), a.primaryBaseURL()) {
					t.Fatalf("Fast escaped Build plane: %s", r.URL.Host)
				}
				return jsonResponse(403, `{"error":{"message":"Fast unavailable"}}`, r), nil
			})
			request := provider.ResponseResourceRequest{Credential: account.Credential{Provider: account.ProviderBuild, EncryptedAccessToken: encrypted, BuildRouteMode: mode, BuildSuperEntitled: true}, Method: http.MethodPost, Path: "/responses", Model: modeldomain.Grok47BuildFast, Operation: "responses", NormalizeBody: true, Body: []byte(`{"input":"hello","model":"grok-4.7-build-fast"}`)}
			res, err := a.ForwardResponse(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			_, _ = io.Copy(io.Discard, res.Body)
			if calls != 1 || res.StatusCode != 403 {
				t.Fatalf("calls=%d status=%d", calls, res.StatusCode)
			}
		})
	}
}
