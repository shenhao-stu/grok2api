package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBodyBudgetReservesBytesAndReleasesAfterRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	entered, release, completed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	router := gin.New()
	router.Use(BodyMemoryBudget(8, 10))
	router.POST("/v1/responses", func(c *gin.Context) {
		if c.GetHeader("X-Hold") == "yes" {
			close(entered)
			<-release
		}
		c.Status(http.StatusNoContent)
	})
	go func() {
		defer close(completed)
		r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("12345678"))
		r.Header.Set("X-Hold", "yes")
		router.ServeHTTP(httptest.NewRecorder(), r)
	}()
	<-entered
	defer func() { close(release); <-completed }()
	for _, tc := range []struct {
		body string
		want int
	}{{"12", 204}, {"123", 503}, {"1", 204}} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(tc.body)))
		if rec.Code != tc.want {
			t.Fatalf("body=%d status=%d", len(tc.body), rec.Code)
		}
		if tc.want == 503 && (rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), "body_memory_busy")) {
			t.Fatal("temporary byte pressure must be explicitly retryable")
		}
	}
}

func TestBodyBudgetUnknownLengthAndPanicCleanup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery(), BodyMemoryBudget(8, 8))
	router.POST("/v1/messages", func(c *gin.Context) {
		if c.GetHeader("X-Panic") == "yes" {
			panic("synthetic")
		}
		_, _ = io.Copy(io.Discard, c.Request.Body)
		c.Status(http.StatusNoContent)
	})
	for _, panics := range []bool{true, false, false} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("1234"))
		req.ContentLength = -1
		if panics {
			req.Header.Set("X-Panic", "yes")
		}
		router.ServeHTTP(rec, req)
		want := 204
		if panics {
			want = 500
		}
		if rec.Code != want {
			t.Fatalf("status=%d want=%d", rec.Code, want)
		}
	}
}

func TestBodyBudgetRejectsDeclaredOverLimitBeforeReading(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/responses", "/v1/chat/completions", "/v1/messages"} {
		router := gin.New()
		router.Use(BodyMemoryBudget(90<<20, 512<<20))
		router.POST(path, func(c *gin.Context) { t.Fatal("oversized request reached inference") })
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		r.ContentLength = (90 << 20) + 1
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		if rec.Code != 413 {
			t.Fatalf("%s status=%d", path, rec.Code)
		}
	}
}
