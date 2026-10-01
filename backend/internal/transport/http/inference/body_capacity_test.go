package inference

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// Exercise transport parsing with no credential, account, or upstream call.
// Invalid model is intentional: a request above the former 32 MiB ceiling must
// now reach JSON/model validation instead of the size rejection path.
func TestThreeTextProtocolsAcceptBodyAboveFormer32MiBLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"padding":"` + strings.Repeat("x", 33<<20) + `"}`
	for _, path := range []string{"/v1/responses", "/v1/chat/completions", "/v1/messages"} {
		router := gin.New()
		router.Use(middleware.MaxBodyBytes(90<<20), middleware.BodyMemoryBudget(90<<20, 512<<20))
		NewHandler(nil, nil, 90<<20).Register(router.Group("/v1"))
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("anthropic-version", "2023-06-01")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s did not reach validation: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}
