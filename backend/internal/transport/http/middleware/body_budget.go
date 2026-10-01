package middleware

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// BodyMemoryBudget bounds retained raw request bytes, not request concurrency.
// Reserve before JSON decoding; handlers can retain multiple decoded copies.
// Unknown-length bodies reserve the individual ceiling rather than trusting an
// attacker-provided length. The Core gateway supplies an exact Content-Length.
func BodyMemoryBudget(maxRequest, capacity int64) gin.HandlerFunc {
	if capacity <= 0 {
		capacity = 512 << 20
	}
	var reserved atomic.Int64
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}
		amount := c.Request.ContentLength
		if amount < 0 {
			amount = maxRequest
		}
		if amount < 0 || amount > maxRequest {
			bodyAdmissionError(c, http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds the configured byte limit.")
			return
		}
		for {
			current := reserved.Load()
			if amount > capacity-current {
				c.Header("Retry-After", "3")
				bodyAdmissionError(c, http.StatusServiceUnavailable, "body_memory_busy", "Request-body memory is busy. Retry shortly.")
				return
			}
			if reserved.CompareAndSwap(current, current+amount) {
				break
			}
		}
		defer reserved.Add(-amount)
		c.Next()
	}
}

func bodyAdmissionError(c *gin.Context, status int, code, message string) {
	if strings.TrimSuffix(c.Request.URL.Path, "/") == "/v1/messages" {
		kind := "invalid_request_error"
		if status == http.StatusServiceUnavailable {
			kind = "overloaded_error"
		}
		c.AbortWithStatusJSON(status, gin.H{"type": "error", "error": gin.H{"type": kind, "code": code, "message": message}})
		return
	}
	kind := "invalid_request_error"
	if status == http.StatusServiceUnavailable {
		kind = "server_error"
	}
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "type": kind, "message": message}})
}
