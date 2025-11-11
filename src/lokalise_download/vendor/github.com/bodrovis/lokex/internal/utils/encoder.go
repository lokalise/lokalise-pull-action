// Package utils holds small helpers shared across the client.
package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// EncodeJSONBody JSON-encodes body into a bytes.Buffer suitable for HTTP requests.
// Notes:
//   - HTML escaping is disabled (e.g., "<" won't become "\u003c") so payloads
//     stay human-readable and match external API expectations.
//   - json.Encoder.Encode appends a trailing newline; that's fine for HTTP bodies.
//   - On encode errors (e.g., unsupported values), returns a wrapped error.
func EncodeJSONBody(body any) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	return &buf, nil
}
