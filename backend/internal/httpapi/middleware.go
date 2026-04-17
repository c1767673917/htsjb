package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"product-collection-form/backend/internal/metrics"
)

const requestIDHeader = "X-Request-Id"

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := randomRequestID()
		c.Set("request_id", requestID)
		c.Writer.Header().Set(requestIDHeader, requestID)
		c.Next()

		fields := []any{
			"request_id", requestID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes_in", c.Request.ContentLength,
			"bytes_out", c.Writer.Size(),
		}
		if year := c.Param("year"); year != "" {
			fields = append(fields, "year", year)
		}
		if orderNo := c.Param("orderNo"); orderNo != "" {
			fields = append(fields, "order_no", orderNo)
		}
		if code := c.GetString("error_code"); code != "" {
			fields = append(fields, "error_code", code)
		}
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		metrics.Default.ObserveHTTPRequest(route, c.Request.Method, c.Writer.Status(), time.Since(start))
		slog.InfoContext(c.Request.Context(), "http_request", fields...)
	}
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		headers := c.Writer.Header()
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Content-Security-Policy", "frame-ancestors 'none'")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("X-Content-Type-Options", "nosniff")
		c.Next()
	}
}

func markErrorCode(c *gin.Context, code string) {
	if code != "" {
		c.Set("error_code", code)
	}
}

func timeoutResponseJSON() string {
	return `{"error":{"code":"INTERNAL","message":"请求处理超时"}}`
}

func randomRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(buf)
}
