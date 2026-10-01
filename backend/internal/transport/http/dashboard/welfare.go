package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Optional Welfare integration: the user's points are read from the billing
// ledger, never reconstructed from Grok's reference USD pricing or retries.
type welfareLedgerDTO struct {
	Start            time.Time   `json:"start"`
	End              time.Time   `json:"end"`
	UpdatedAt        time.Time   `json:"updatedAt"`
	Timezone         string      `json:"timezone"`
	Unit             string      `json:"unit"`
	Redeemed         json.Number `json:"redeemed"`
	Checkin          json.Number `json:"checkin"`
	Granted          json.Number `json:"granted"`
	Expired          json.Number `json:"expired"`
	Consumed         json.Number `json:"consumed"`
	InventoryCreated json.Number `json:"inventoryCreated"`
	Requests         int64       `json:"requests"`
	Successful       int64       `json:"successful"`
	Failed           int64       `json:"failed"`
	Pending          int64       `json:"pending"`
}

func welfareLedger(ctx context.Context) (*welfareLedgerDTO, string) {
	secret := os.Getenv("WELFARE_LEDGER_SECRET")
	if len(secret) < 32 {
		return nil, "disabled"
	}
	now := time.Now()
	bj := now.In(time.FixedZone("Asia/Shanghai", 8*3600))
	start := time.Date(bj.Year(), bj.Month(), bj.Day(), 0, 0, 0, 0, bj.Location())
	query := url.Values{"start": {start.Format(time.RFC3339)}, "end": {now.Format(time.RFC3339)}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://welfare-core:3000/internal/welfare/ledger?"+query.Encode(), nil)
	if err != nil {
		return nil, "unavailable"
	}
	request.Header.Set("X-Welfare-Ledger", secret)
	client := http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(request)
	if err != nil {
		return nil, "unavailable"
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, "unavailable"
	}
	var out welfareLedgerDTO
	if json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&out) != nil || out.Unit != "points" {
		return nil, "unavailable"
	}
	return &out, "live"
}
