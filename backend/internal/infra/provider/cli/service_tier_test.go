package cli

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/infra/provider"
)

func TestBuildRejectsUnverifiedServiceTierBeforeAllProtocolConversions(t *testing.T) {
	for _, operation := range []string{"chat", "messages", "responses"} {
		for _, value := range []string{`"priority"`, `"flex"`, `"fast"`, `"ultrafast"`, `""`, `true`, `42`, `{}`} {
			t.Run(operation+"/"+value, func(t *testing.T) {
				// No cipher/account/transport is needed: rejection precedes upstream activity.
				a := &Adapter{}
				response, err := a.ForwardResponse(context.Background(), provider.ResponseResourceRequest{Operation: operation, Method: http.MethodPost, Path: "/responses", Model: "grok-4.7", NormalizeBody: true, Body: []byte(`{"model":"grok-4.7","input":"hello","service_tier":` + value + `}`)})
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				_, _ = io.Copy(io.Discard, response.Body)
				if response.StatusCode != 400 {
					t.Fatalf("status = %d", response.StatusCode)
				}
			})
		}
	}
}

func TestBuildDefaultServiceTierRemainsCompatible(t *testing.T) {
	for _, body := range []string{`{}`, `{"service_tier":null}`, `{"service_tier":"auto"}`, `{"service_tier":"default"}`, `{"metadata":{"service_tier":"priority"}}`} {
		if err := validateBuildServiceTier(provider.ResponseResourceRequest{Body: []byte(body)}); err != nil {
			t.Fatalf("valid default rejected: %s %v", body, err)
		}
	}
}
