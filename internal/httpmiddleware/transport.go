package httpmiddleware

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"time"
)

// DefaultTransport returns a configured http.Transport suitable for external API calls.
func DefaultTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// Middleware is a function that wraps an http.RoundTripper.
type Middleware func(http.RoundTripper) http.RoundTripper

// Wrap wraps a base http.RoundTripper with a chain of middlewares.
// Middlewares are applied in order, so the first middleware is the outermost.
func Wrap(base http.RoundTripper, middlewares ...Middleware) http.RoundTripper {
	for i := len(middlewares) - 1; i >= 0; i-- {
		base = middlewares[i](base)
	}
	return base
}

// RequestGetBodySetter is a middleware that ensures request.GetBody is set.
// This is required for automatic retry logic and redirect handling.
func RequestGetBodySetter(next http.RoundTripper) http.RoundTripper {
	return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			req.Body.Close()
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(body)), nil
			}
		}
		return next.RoundTrip(req)
	})
}
