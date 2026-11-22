package slack

import "bytes"

// Present is a field read only for whether it is there, as JavaScript's
// !!value reads it: true for any value but null, false, "" and 0. What
// the value says isn't kept.
type Present bool

// UnmarshalJSON records whether data is a truthy value.
func (p *Present) UnmarshalJSON(data []byte) error {
	switch string(bytes.TrimSpace(data)) {
	case "null", "false", `""`, "0":
		*p = false
	default:
		*p = true
	}
	return nil
}
