package dashboard

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type ledgerTransport func(*http.Request) (*http.Response, error)

func (f ledgerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWelfareLedgerUsesPrivateSourceAndAcceptsDecimalNumbers(t *testing.T) {
	t.Setenv("WELFARE_LEDGER_SECRET", strings.Repeat("x", 32))
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	status := 200
	http.DefaultTransport = ledgerTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "welfare-core:3000" || r.URL.Path != "/internal/welfare/ledger" || r.Header.Get("X-Welfare-Ledger") == "" {
			t.Fatal("ledger source boundary changed")
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"unit":"points","granted":23.5,"consumed":"1.25","redeemed":20,"checkin":3.5,"expired":0,"inventoryCreated":200}`)), Header: make(http.Header)}, nil
	})
	data, state := welfareLedger(context.Background())
	if state != "live" || data.Granted.String() != "23.5" || data.Consumed.String() != "1.25" {
		t.Fatal("numeric ledger rejected")
	}
	status = 503
	if data, state = welfareLedger(context.Background()); data != nil || state != "unavailable" {
		t.Fatal("failure presented as live zero balance")
	}
}
