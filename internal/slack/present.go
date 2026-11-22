package slack

import (
	"bytes"
	"encoding/json"
)

// Truthy is JavaScript's !!value for a JSON value as sent: false for a
// missing one, null, false, "" and 0, true for anything else (an object,
// an array, a non-empty string, a non-zero number).
func Truthy(v json.RawMessage) bool {
	switch s := string(bytes.TrimSpace(v)); s {
	case "", "null", "false", `""`:
		return false
	default:
		var f float64
		if json.Unmarshal([]byte(s), &f) == nil && f == 0 { // 0, -0, 0.0, 0e5
			return false
		}
		return true
	}
}
