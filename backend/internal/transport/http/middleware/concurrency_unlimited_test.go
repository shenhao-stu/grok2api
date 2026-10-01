package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestUnlimitedGateRetainsActiveAccountingAcrossHotReload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gate := NewConcurrencyGate(-1)
	router := gin.New()
	router.Use(gate.Middleware())
	router.GET("/", func(c *gin.Context) {
		gate.mu.Lock()
		active := gate.active
		gate.mu.Unlock()
		if active != 101 {
			t.Errorf("active=%d, want existing 100 plus this request", active)
		}
		c.Status(http.StatusOK)
	})
	gate.active = 100
	request := func() int {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		return recorder.Code
	}
	if request() != http.StatusOK || gate.active != 100 {
		t.Fatal("unlimited gate rejected or lost active accounting")
	}
	gate.UpdateLimit(50)
	if request() != http.StatusServiceUnavailable || gate.active != 100 {
		t.Fatal("lowering limit must preserve in-flight requests and reject new ones")
	}
	gate.UpdateLimit(-1)
	if request() != http.StatusOK || gate.active != 100 {
		t.Fatal("unlimited setting did not hot-reload")
	}
}
