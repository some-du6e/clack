package sentry

import (
	"runtime"
	"strings"
	"time"
)

// Event is the part of Sentry's event payload Clack fills: what happened
// (a message, an exception), where (release, platform, the runtime
// contexts) and the fields a report chose (tags, contexts). There is no
// user, request, hostname (server_name) or breadcrumb field, so none can
// be sent.
type Event struct {
	EventID     string            `json:"event_id"`
	Timestamp   string            `json:"timestamp"`
	Platform    string            `json:"platform"`
	Level       string            `json:"level"`
	Type        string            `json:"type,omitempty"` // "feedback" for a report
	Logger      string            `json:"logger,omitempty"`
	Release     string            `json:"release,omitempty"`
	Environment string            `json:"environment,omitempty"`
	Message     *Message          `json:"message,omitempty"`
	Exception   *Exceptions       `json:"exception,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Contexts    map[string]any    `json:"contexts,omitempty"`
	SDK         *SDK              `json:"sdk,omitempty"`
}

// Message is an event's message.
type Message struct {
	Formatted string `json:"formatted"`
}

// Exceptions is an event's exception list, the crashing one last.
type Exceptions struct {
	Values []Exception `json:"values"`
}

// Exception is one exception: its type ("panic", "fatal error"), its
// value (the message) and where it happened.
type Exception struct {
	Type       string      `json:"type"`
	Value      string      `json:"value"`
	Stacktrace *Stacktrace `json:"stacktrace,omitempty"`
	Mechanism  *Mechanism  `json:"mechanism,omitempty"`
}

// Mechanism says how the exception was caught: a crash, unhandled.
type Mechanism struct {
	Type    string `json:"type"`
	Handled bool   `json:"handled"`
}

// Stacktrace is a goroutine's frames, oldest call first (Sentry's order).
type Stacktrace struct {
	Frames []Frame `json:"frames"`
}

// Frame is one call: the function in its package (module), the file
// (module-relative in release builds, -trimpath; a home directory is ~)
// and line. Arguments and addresses aren't kept.
type Frame struct {
	Function string `json:"function,omitempty"`
	Module   string `json:"module,omitempty"`
	Filename string `json:"filename,omitempty"`
	Lineno   int    `json:"lineno,omitempty"`
	InApp    bool   `json:"in_app"`
}

// SDK names the sender: Clack's own (this package), not a Sentry SDK.
type SDK struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// fill sets what every event of c's carries: an id and a timestamp if it
// has none, the release, the platform and the runtime contexts.
func (ev *Event) fill(c *Client) {
	if ev.EventID == "" {
		ev.EventID = newEventID()
	}
	if ev.Timestamp == "" {
		ev.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if ev.Platform == "" {
		ev.Platform = "go"
	}
	if ev.Level == "" {
		ev.Level = "error"
	}
	ev.Release = c.release
	ev.Environment = "release"
	if c.version == "dev" {
		ev.Environment = "dev"
	}
	ev.SDK = &SDK{Name: "clack.sentry", Version: c.version}
	if ev.Contexts == nil {
		ev.Contexts = map[string]any{}
	}
	for k, v := range runtimeContexts() {
		if _, ok := ev.Contexts[k]; !ok {
			ev.Contexts[k] = v
		}
	}
	if ev.Tags == nil {
		ev.Tags = map[string]string{}
	}
	ev.Tags["goos"], ev.Tags["goarch"] = runtime.GOOS, runtime.GOARCH
}

// scrub runs Scrub over every string the event carries that came from
// outside this package (messages, values, file names, tags, contexts).
func (ev *Event) scrub() {
	if ev.Message != nil {
		ev.Message.Formatted = Scrub(ev.Message.Formatted)
	}
	if ev.Exception != nil {
		for i := range ev.Exception.Values {
			x := &ev.Exception.Values[i]
			x.Type, x.Value = Scrub(x.Type), Scrub(x.Value)
			if x.Stacktrace != nil {
				for j := range x.Stacktrace.Frames {
					f := &x.Stacktrace.Frames[j]
					f.Function, f.Module, f.Filename = Scrub(f.Function), Scrub(f.Module), Scrub(f.Filename)
				}
			}
		}
	}
	for k, v := range ev.Tags {
		ev.Tags[k] = tagValue(Scrub(v))
	}
	for k, v := range ev.Contexts {
		ev.Contexts[k] = scrubAny(v)
	}
}

// scrubAny scrubs the strings in a context's value (maps, slices and
// strings; anything else as it is).
func scrubAny(v any) any {
	switch x := v.(type) {
	case string:
		return Scrub(x)
	case []string:
		out := make([]string, len(x))
		for i, s := range x {
			out[i] = Scrub(s)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(x))
		for k, s := range x {
			out[k] = Scrub(s)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, s := range x {
			out[k] = scrubAny(s)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = scrubAny(s)
		}
		return out
	}
	return v
}

// tagValue is a tag's value as Sentry takes it: one line, at most 200
// characters.
func tagValue(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:199]) + "…"
	}
	return s
}

// ReportRow is one row of an inspect report (inspect.Line): a key and its
// value, already free of content (ids are salted one-way tags).
type ReportRow struct{ Key, Value string }

// Feedback is a report from the report box: the reporter's comment and
// the inspect report's rows, as Sentry user feedback (an envelope item
// of type "feedback": contexts.feedback.message is the comment). The
// rows go in the "report" context and the main ones as tags, to search
// by.
func Feedback(comment string, rows []ReportRow, shot bool) *Event {
	ev := &Event{Type: "feedback", Level: "info", Logger: "clack.report"}
	report := map[string]string{}
	tags := map[string]string{"kind": "report"}
	for _, r := range rows {
		if r.Key == "" || r.Value == "" {
			continue
		}
		report[r.Key] = r.Value
		switch r.Key {
		case "component", "region", "capture":
			tags[r.Key] = r.Value
		}
	}
	if shot {
		tags["screenshot"] = "yes"
	} else {
		tags["screenshot"] = "no"
	}
	ev.Tags = tags
	ev.Contexts = map[string]any{
		"feedback": map[string]any{"message": comment, "source": "clack report box (Hyper+R)"},
		"report":   report,
	}
	return ev
}
