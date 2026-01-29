// Package apierr: parsing helpers for Lokalise-style error payloads.
//
// Lokalise (and some proxies) may return different JSON error shapes.
// Parse() tries several known patterns and falls back to a generic form,
// populating APIError with best-effort fields for callers to inspect.
package apierr

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Parse converts an HTTP error body (already size-limited by caller) and
// the HTTP status code into a structured *APIError.
//   - slurp: raw response body bytes (may be empty or non-JSON)
//   - status: HTTP status code from the response
//
// Supported shapes (examples):
//  1. Top-level fields:
//     {"message":"msg","statusCode":429,"error":"Too Many Requests"}
//  2. Nested error:
//     {"error":{"message":"msg","code":429,"details":{"bucket":"global"}}}
//  3. Alternate top-level with code/errorCode (number or string):
//     {"message":"msg","code":"429","details":{...}}
//  4. Fallback: preserve "message" and "error" (string) if present; stash all fields in Details.
//
// Non-JSON bodies produce an APIError with Reason "non-json error body" and Raw=trimmed body.
func Parse(slurp []byte, status int) *APIError {
	trimmed := strings.TrimSpace(string(slurp))

	// Non-JSON fallback (empty or not starting with { / [).
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return &APIError{
			Status:  status,
			Message: http.StatusText(status),
			Reason:  "non-json error body",
			Raw:     trimmed,
		}
	}

	// Decode with UseNumber so numbers aren't forced to float64.
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()

	var anyJSON any
	if err := dec.Decode(&anyJSON); err != nil {
		return &APIError{
			Status:  status,
			Message: http.StatusText(status),
			Reason:  "invalid json in error body",
			Raw:     trimmed,
			Details: map[string]any{"unmarshal_error": err.Error()},
		}
	}

	obj, _ := anyJSON.(map[string]any)

	// 1) Top-level: { message: string, statusCode: number, error: string }
	if msg, ok := getString(obj, "message"); ok {
		if sc, ok := getNumberAsInt(obj, "statusCode"); ok {
			if reason, ok := getString(obj, "error"); ok {
				return &APIError{
					Status:  status,
					Code:    sc,
					Message: msg,
					Reason:  reason,
					Raw:     trimmed,
					Details: obj,
				}
			}
		}
	}

	// 2) Nested: { error: { message, code, details } }
	if errAny, ok := obj["error"].(map[string]any); ok {
		msg, _ := getString(errAny, "message")
		code, okCode := getNumberAsInt(errAny, "code")
		if !okCode {
			code = status
		}

		var details map[string]any
		if detObj, ok := errAny["details"].(map[string]any); ok {
			details = detObj // pass-through object
		} else if det, ok := errAny["details"]; ok {
			details = map[string]any{"details": det} // preserve non-object detail
		} else {
			details = map[string]any{"reason": "server error without details"}
		}

		return &APIError{
			Status:  status,
			Code:    code,
			Message: coalesce(msg, http.StatusText(status)),
			Raw:     trimmed,
			Details: details,
		}
	}

	// 3) Alt top-level: { message: string, code?: number|string, errorCode?: number|string, details?: any }
	if msg, ok := getString(obj, "message"); ok {
		if code, ok := getNumberAsInt(obj, "code"); ok {
			return &APIError{
				Status:  status,
				Code:    code,
				Message: msg,
				Raw:     trimmed,
				Details: pickDetails(obj),
			}
		}
		if code, ok := getNumberAsInt(obj, "errorCode"); ok {
			return &APIError{
				Status:  status,
				Code:    code,
				Message: msg,
				Raw:     trimmed,
				Details: pickDetails(obj),
			}
		}
	}

	// 4) Fallback with reason if "error" is a string.
	reason, _ := getString(obj, "error")
	return &APIError{
		Status:  status,
		Message: coalesce(getStringOr(obj, "message", ""), http.StatusText(status)),
		Reason:  coalesce(reason, "unhandled error format"),
		Raw:     trimmed,
		Details: obj,
	}
}

// pickDetails extracts a details object if present; if "details" exists but
// is not an object, it is wrapped under {"details": <value>} to preserve it.
// If absent, returns a minimal sentinel map.
func pickDetails(obj map[string]any) map[string]any {
	if detObj, ok := obj["details"].(map[string]any); ok {
		return detObj
	}
	if det, ok := obj["details"]; ok {
		return map[string]any{"details": det}
	}
	return map[string]any{"reason": "server error without details"}
}

// coalesce returns the first non-empty string from ss (or "").
func coalesce(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// getString fetches a string value at key from m, with ok=true on success.
func getString(m map[string]any, key string) (string, bool) {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}

// getStringOr returns m[key] as string when present/typed, otherwise def.
func getStringOr(m map[string]any, key, def string) string {
	if s, ok := getString(m, key); ok {
		return s
	}
	return def
}

// getNumberAsInt extracts an integer from common JSON number encodings:
// json.Number, float64, int, or string containing digits (e.g., "429").
// Returns (value, true) on success; otherwise (0, false).
func getNumberAsInt(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case json.Number:
		if i, err := strconv.Atoi(n.String()); err == nil {
			return i, true
		}
	case float64:
		return int(n), true
	case int:
		return n, true
	case string:
		// accept numeric strings: "429", "500"
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, true
		}
	}
	return 0, false
}
