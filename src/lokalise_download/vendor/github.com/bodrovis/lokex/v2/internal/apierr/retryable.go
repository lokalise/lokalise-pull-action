// apierr/retryable.go
package apierr

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"
)

var (
	jitterRandMu sync.Mutex
	jitterRand   = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// IsRetryable returns true only for transient failures.
// Order is IMPORTANT.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// 1) Real network timeouts from the net stack (dial/read/TLS): *net.OpError
	var op *net.OpError
	if errors.As(err, &op) && op.Timeout() {
		return true
	}

	// 2) Pure context budget errors → NOT retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// 3) Other Timeout()-ish errors (e.g., url.Error, custom mocks) → retryable
	var hasTimeout interface{ Timeout() bool }
	if errors.As(err, &hasTimeout) && hasTimeout.Timeout() {
		return true
	}

	// 4) Flaky transport / short reads → retryable
	if errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNABORTED) {
		return true
	}

	// 5) Retryable HTTP statuses → retryable
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Status {
		case http.StatusRequestTimeout, // 408
			http.StatusTooEarly,            // 425
			http.StatusTooManyRequests,     // 429
			http.StatusInternalServerError, // 500
			http.StatusBadGateway,          // 502
			http.StatusServiceUnavailable,  // 503
			http.StatusGatewayTimeout:      // 504
			return true
		}
	}

	return false
}

// JitteredBackoff returns a randomized delay in [0.5*base, 1.5*base).
// If base <= 0, it falls back to 300ms.
//
// Note: we intentionally use a package-local PRNG guarded by a mutex.
// A *rand.Rand created via rand.New(...) is NOT goroutine-safe, so without
// the lock we'd get races when multiple retries happen concurrently.
func JitteredBackoff(base time.Duration) time.Duration {
	if base <= 0 {
		base = 300 * time.Millisecond
	}

	jitterRandMu.Lock()
	delta := time.Duration(jitterRand.Int63n(int64(base))) // [0, base)
	jitterRandMu.Unlock()

	return base/2 + delta
}
