package audit

import (
	"strconv"
	"testing"
)

func TestGrok47OfficialPricesPreserveFastTierAndContextBoundary(t *testing.T) {
	for _, tc := range []struct {
		model                string
		context              int64
		input, cache, output int64
	}{
		{"grok-4.7", 199_999, 20_000, 5_000, 60_000},
		{"grok-4.7", 200_000, 40_000, 10_000, 120_000},
		{"grok-4.7", 200_001, 40_000, 10_000, 120_000},
		{"grok-4.6", 199_999, 20_000, 5_000, 60_000},
		{"grok-4.6", 200_000, 40_000, 10_000, 120_000},
		{"grok-4.5", 199_999, 20_000, 3_000, 60_000},
		{"grok-4.5", 200_000, 40_000, 6_000, 120_000},
		{"Build/grok-4.7-build-fast", 200_000, 40_000, 10_000, 120_000},
		{"Build/grok-4.7-build-fast", 200_001, 60_000, 15_000, 180_000},
		{"Build/grok-4.7-build-fast-xhigh", 200_001, 60_000, 15_000, 180_000},
	} {
		t.Run(tc.model+"/"+strconv.FormatInt(tc.context, 10), func(t *testing.T) {
			want := 100*tc.input + 50*tc.cache + 20*tc.output
			got, ok := EstimateOfficialCost(tc.model, 150, 50, 20, tc.context)
			if !ok || got.CostInUSDTicks != want {
				t.Fatalf("cost = %#v, want %d", got, want)
			}
			breakdown, ok := reconstructTextCost(tc.model, 150, 50, 20, tc.context)
			if !ok || breakdown.CostInUSDTicks != want {
				t.Fatalf("breakdown = %#v, want %d", breakdown, want)
			}
		})
	}
	for _, unknown := range []string{"grok-4.7-fast", "grok-4.7-build-fast-unknown", "grok-4.70", "Other/grok-4.7"} {
		if price, ok := EstimateOfficialCost(unknown, 100, 0, 10, 100); ok {
			t.Fatalf("unknown variant priced as standard: %s %#v", unknown, price)
		}
	}
}
