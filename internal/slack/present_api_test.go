package slack

import "encoding/json"

// The initial_comment API (red branches stub these).
func truthy(v json.RawMessage) bool { return Truthy(v) }
func initialComment(f *File) string { return string(f.InitialComment) }
