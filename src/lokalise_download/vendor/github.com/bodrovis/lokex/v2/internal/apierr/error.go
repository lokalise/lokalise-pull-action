// Package apierr: error defines a typed API error used across lokex. It captures the
// HTTP status, a service-specific error code (when provided), a human-readable
// message, optional machine-friendly reason/details, and the raw response body.
// The original *http.Response is kept for headers/status, but its Body is
// already fully consumed by the caller (do not read it again).
package apierr

import (
	"net/http"
)

// APIError represents a non-2xx response from the Lokalise API (or other
// HTTP services used by lokex). Callers can inspect Status/Code/Details to
// decide how to handle the error (e.g., retry on 429/5xx as determined by
// apierr.IsRetryable).
type APIError struct {
	// Status is the HTTP status code (e.g., 400, 429, 500).
	Status int

	// Code is a service-specific numeric code, if the API returned one.
	// When absent in the payload, Code typically mirrors Status.
	Code int

	// Message is a human-readable error summary from the server (if provided).
	// Error() returns Message when non-empty, otherwise it falls back to the
	// standard HTTP status text for Status.
	Message string

	// Reason is an optional short machine-friendly identifier (when provided
	// by the server). Not all responses include it.
	Reason string

	// Details holds arbitrary structured data returned by the API (e.g.,
	// rate-limit bucket info). It is safe to nil-check and type-assert as needed.
	Details map[string]any

	// Raw is the trimmed raw response body as a string, useful for debugging
	// or logging when decoding failed or fields were missing.
	Raw string

	// Resp is the original HTTP response for access to headers/status/etc.
	// The body has already been fully read/consumed upstream; do not read it.
	Resp *http.Response
}

// Error implements the error interface.
// It prefers the server-provided Message; when empty, it falls back to the
// canonical HTTP status text for Status.
func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.Status)
}
