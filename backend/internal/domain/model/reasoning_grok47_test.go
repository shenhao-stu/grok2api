package model

import (
	"slices"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
)

func TestGrok47ReasoningPreservesFastIdentity(t *testing.T) {
	want := []string{"low", "medium", "high", "xhigh"}
	for _, model := range []string{"grok-4.7", Grok47BuildFast, "Build/" + Grok47BuildFast} {
		if got := SupportedReasoningEffortsForProvider(account.ProviderBuild, model); !slices.Equal(got, want) {
			t.Fatalf("%s efforts = %v", model, got)
		}
		base, effort, ok := ParseReasoningModelAlias(model + "-xhigh")
		if !ok || base != model || effort != "xhigh" {
			t.Fatalf("alias changed execution tier: %s %s %v", base, effort, ok)
		}
		if SupportsReasoningEffort(model, "none") || SupportsReasoningEffort(model, "max") {
			t.Fatal("Grok 4.7 does not expose none/max reasoning aliases")
		}
	}
}
