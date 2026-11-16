package applog

import (
	"strings"
	"testing"
)

// Cookie:, Set-Cookie: and Authorization: values go wherever the name is a
// header, and nowhere else: the old rule matched the word anywhere, so
// "notify: macOS notification authorization: granted true" reached the
// log as "…authorization: REDACTED" (ten times on Karim's Mac, 11-14 to
// 11-15) and hid the permission result.
func TestHeaderRedactionOnlyInHeaderContext(t *testing.T) {
	masked := []string{
		"Authorization: Basic hdrSECRET",
		"authorization: Basic hdrSECRET",
		"  Cookie: d=hdrSECRET; lc=hdrSECRET",
		"Set-Cookie: x=hdrSECRET; Path=/",
		"slack: request headers map[Authorization:[Basic hdrSECRET] Cookie:[x=hdrSECRET]]",
		"rtm: dial: Cookie: x=hdrSECRET",
		`taut: fetch {"headers":"Cookie: x=hdrSECRET"}`,
		"sent header Authorization: Basic hdrSECRET",
		"sent header Set-Cookie: x=hdrSECRET",
		"GET / HTTP/1.1\r\ncookie: x=hdrSECRET",
		"proxy, authorization: Basic hdrSECRET",
	}
	for _, l := range masked {
		got := Redact(l)
		if strings.Contains(got, "hdrSECRET") {
			t.Errorf("Redact(%q) = %q: header value kept", l, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Errorf("Redact(%q) = %q: no REDACTED", l, got)
		}
	}
	kept := []string{
		"notify: macOS notification permission: granted=true",
		"notify: macOS notification permission error: UNErrorDomain 1 Notifications are not allowed",
		"notify: macOS notification authorization: granted true",
		"slack: Slack rejected the token and cookie: invalid_auth",
		"slack: Slack didn't accept that d cookie: invalid_auth",
	}
	for _, l := range kept {
		if got := Redact(l); got != l {
			t.Errorf("Redact(%q) = %q: prose redacted as a header", l, got)
		}
	}
	// A credential in a prose line is still caught by the other rules.
	for _, l := range []string{
		"slack: the token and cookie: xoxc-123-protoSECRET",
		"notify authorization: Bearer protoSECRET",
	} {
		if got := Redact(l); strings.Contains(got, "protoSECRET") {
			t.Errorf("Redact(%q) = %q", l, got)
		}
	}
}
