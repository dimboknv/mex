package httpmiddleware

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// RoundTripperFunc is a function that implements http.RoundTripper
type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Logger creates a logging middleware for http.RoundTripper.
// maxBodySize controls body logging:
//   - 0: no body logging
//   - -1: log entire body
//   - >0: log first N bytes of body
func Logger(logger *slog.Logger, maxBodySize int) func(http.RoundTripper) http.RoundTripper {
	return func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			// Log request
			logRequest(logger, req, maxBodySize)

			// Execute request
			start := time.Now()
			resp, err := next.RoundTrip(req)
			duration := time.Since(start)

			// Log response
			if err != nil {
				logger.Error("HTTP request failed",
					slog.String("method", req.Method),
					slog.String("url", req.URL.String()),
					slog.Duration("duration", duration),
					slog.Any("error", err))

				return resp, err
			}

			logResponse(logger, req, resp, duration, maxBodySize)

			return resp, nil
		})
	}
}

// logRequest logs HTTP request details
func logRequest(logger *slog.Logger, req *http.Request, maxBodySize int) {
	attrs := []slog.Attr{
		slog.String("method", req.Method),
		slog.String("url", req.URL.String()),
		slog.String("proto", req.Proto),
		slog.String("host", req.Host),
	}

	// Log headers
	if len(req.Header) > 0 {
		headerAttrs := make([]slog.Attr, 0, len(req.Header))
		for k, v := range req.Header {
			// Skip sensitive headers
			if isSensitiveHeader(k) {
				headerAttrs = append(headerAttrs, slog.String(k, "[REDACTED]"))
			} else {
				headerAttrs = append(headerAttrs, slog.String(k, strings.Join(v, ", ")))
			}
		}

		attrs = append(attrs, slog.Any("headers", slog.GroupValue(headerAttrs...)))
	}

	// Log query params
	if req.URL.RawQuery != "" {
		attrs = append(attrs, slog.String("query", req.URL.RawQuery))
	}

	// Log body if needed
	if maxBodySize != 0 && req.Body != nil && req.Body != http.NoBody {
		body, err := readBody(req.Body, maxBodySize)
		if err == nil && len(body) > 0 {
			// Restore body for next handler
			req.Body = io.NopCloser(bytes.NewBuffer(body))

			// Log body as string (slog will handle JSON formatting)
			attrs = append(attrs, slog.String("body", string(body)))
		}
	}

	logger.LogAttrs(req.Context(), slog.LevelDebug, "📤 HTTP Request", attrs...)
}

// logResponse logs HTTP response details
func logResponse(logger *slog.Logger, req *http.Request, resp *http.Response, duration time.Duration, maxBodySize int) {
	attrs := []slog.Attr{
		slog.String("method", req.Method),
		slog.String("url", req.URL.String()),
		slog.Int("status", resp.StatusCode),
		slog.String("status_text", resp.Status),
		slog.Duration("duration", duration),
	}

	// Log headers
	if len(resp.Header) > 0 {
		headerAttrs := make([]slog.Attr, 0, len(resp.Header))
		for k, v := range resp.Header {
			headerAttrs = append(headerAttrs, slog.String(k, strings.Join(v, ", ")))
		}

		attrs = append(attrs, slog.Any("headers", slog.GroupValue(headerAttrs...)))
	}

	// Log body if needed
	if maxBodySize != 0 && resp.Body != nil {
		body, err := readBody(resp.Body, maxBodySize)
		if err == nil && len(body) > 0 {
			// Restore body for caller
			resp.Body = io.NopCloser(bytes.NewBuffer(body))

			// Log body as string (slog will handle JSON formatting)
			attrs = append(attrs, slog.String("body", string(body)))
		}
	}

	// Determine log level based on status code
	level := slog.LevelDebug
	if resp.StatusCode >= 400 {
		level = slog.LevelWarn
	}

	if resp.StatusCode >= 500 {
		level = slog.LevelError
	}

	logger.LogAttrs(req.Context(), level, "📥 HTTP Response", attrs...)
}

// readBody reads the body up to maxBodySize bytes
func readBody(body io.ReadCloser, maxBodySize int) ([]byte, error) {
	defer body.Close()

	if maxBodySize == -1 {
		// Read entire body
		return io.ReadAll(body)
	}

	// Read up to maxBodySize bytes
	buf := make([]byte, maxBodySize)
	n, err := io.ReadFull(body, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	return buf[:n], nil
}

// isSensitiveHeader checks if header contains sensitive information
func isSensitiveHeader(name string) bool {
	sensitiveHeaders := []string{
		"authorization",
		"cookie",
		"set-cookie",
		"x-api-key",
		"x-auth-token",
		"x-csrf-token",
	}

	lowerName := strings.ToLower(name)
	for _, sensitive := range sensitiveHeaders {
		if lowerName == sensitive {
			return true
		}
	}

	return false
}
