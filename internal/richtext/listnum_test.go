package richtext

import (
	"strings"
	"testing"
	"time"
)

// listMsg is a message whose one block is a rich_text_list of style at
// indent and offset (JSON numbers as a sender wrote them), its items "x"
// and "y".
func listMsg(style, indent, offset string) string {
	item := func(s string) string {
		return `{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}`
	}
	return `{"ts":"1.0","text":"t","blocks":[{"type":"rich_text","elements":[{"type":"rich_text_list","style":"` + style +
		`","indent":` + indent + `,"offset":` + offset + `,"elements":[` + item("x") + `,` + item("y") + `]}]}]}`
}

// bounded runs f and fails t if it panics or is still running after d.
func bounded(t *testing.T, what string, d time.Duration, f func()) {
	t.Helper()
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		f()
	}()
	select {
	case p := <-done:
		if p != nil {
			t.Errorf("%s panicked: %v", what, p)
		}
	case <-time.After(d):
		t.Errorf("%s still running after %v", what, d)
	}
}

// TestListHugeOffset (the review of bkc-128): a sender's ordered list at
// indent 2 (lower-roman) with a huge offset crashed Clack when its toast
// was written (convertNumberToRomanNumeral's "m" per thousand:
// strings.Repeat past makeslice), and an offset of 1e9 asked for a
// megabyte of "m" per item. Slack draws the list with counter-reset
// "list-2 {offset}", which Chromium keeps in 32 bits, and lower-roman
// falls back to decimal above 3999; a negative offset sets no counter
// (the count starts at 1). The toast's text and the plain text count the
// same way, in bounded time and size.
func TestListHugeOffset(t *testing.T) {
	for _, c := range []struct{ offset, toast, plain string }{
		{"3997", "mmmcmxcviii. x.\nmmmcmxcix. y.", "3998. x\n3999. y"},
		{"3998", "mmmcmxcix. x.\n4000. y.", "3999. x\n4000. y"},
		{"999999999", "1000000000. x.\n1000000001. y.", "1000000000. x\n1000000001. y"},
		{"4611686018427387904", "2147483647. x.\n2147483647. y.", "2147483647. x\n2147483647. y"},
		{"9223372036854775807", "2147483647. x.\n2147483647. y.", "2147483647. x\n2147483647. y"},
		{"-5", "i. x.\nii. y.", "1. x\n2. y"},
	} {
		m := msg(t, listMsg("ordered", "2", c.offset))
		var toast, plain string
		bounded(t, "offset "+c.offset, 5*time.Second, func() {
			d := FromMessage(R, m)
			toast = NotificationText(R, m, d, nil)
			plain = PlainText(d)
		})
		if toast != c.toast || len(toast) > 64 {
			t.Errorf("offset %s: toast text %.80q, want %q", c.offset, toast, c.toast)
		}
		if trimLines(plain) != c.plain || len(plain) > 128 {
			t.Errorf("offset %s: plain text %.80q, want %q", c.offset, plain, c.plain)
		}
	}
}

// TestListIndentOutOfRange: a list's indent as Slack's client takes it,
// 0 to 8. A negative one panicked in the plain text (strings.Repeat) and
// in a toast's bullet (the marker table indexed at indent%3 < 0); a huge
// one wrote four spaces per level.
func TestListIndentOutOfRange(t *testing.T) {
	for _, c := range []struct{ style, indent, toast, plain string }{
		{"bullet", "-1", "• x.\n• y.", "• x\n• y"},
		{"ordered", "-4", "1. x.\n2. y.", "1. x\n2. y"},
		{"bullet", "99", "■ x.\n■ y.", strings.Repeat("    ", 8) + "• x\n" + strings.Repeat("    ", 8) + "• y"},
	} {
		m := msg(t, listMsg(c.style, c.indent, "0"))
		var toast, plain string
		var indent int
		bounded(t, c.style+" indent "+c.indent, 5*time.Second, func() {
			d := FromMessage(R, m)
			if len(d.Blocks) > 0 {
				indent = d.Blocks[0].Indent
			}
			toast = NotificationText(R, m, d, nil)
			plain = PlainText(d)
		})
		if indent < 0 || indent > 8 {
			t.Errorf("%s indent %s: decoded as %d, want 0-8", c.style, c.indent, indent)
		}
		if toast != c.toast {
			t.Errorf("%s indent %s: toast text %q, want %q", c.style, c.indent, toast, c.toast)
		}
		if strings.TrimRight(plain, "\n") != c.plain {
			t.Errorf("%s indent %s: plain text %q, want %q", c.style, c.indent, plain, c.plain)
		}
	}
}

// trimLines is s with each line's leading and trailing space cut.
func trimLines(s string) string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range ls {
		ls[i] = strings.TrimSpace(ls[i])
	}
	return strings.Join(ls, "\n")
}
