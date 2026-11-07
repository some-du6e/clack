package slack

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

// Session secrets must never reach logs, error strings or crash output:
// xoxc/xoxd/xoxe/xoxp/xoxb tokens, the d and d-s cookies, and token=
// query or form values (the realtime gateway URL carries the token).

var secretRE = regexp.MustCompile(`xox[a-z]-[^\s"'&;,)<>\]]+|\b(?i:token|enterprise_token|d|d-s)=[^\s"'&;,)<>\]]+`)

// Redact replaces anything that looks like a Slack token or session cookie
// in s with a placeholder.
func Redact(s string) string {
	if !strings.Contains(s, "xox") && !strings.Contains(s, "=") {
		return s
	}
	return secretRE.ReplaceAllStringFunc(s, func(m string) string {
		if i := strings.IndexByte(m, '='); i >= 0 && !strings.HasPrefix(strings.ToLower(m), "xox") {
			return m[:i+1] + "REDACTED"
		}
		return m[:4] + "-REDACTED"
	})
}

// redactedError hides secrets in err's message but still unwraps, so
// errors.Is(err, context.Canceled) and friends keep working.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// scrubErr returns err with any token or cookie in its text redacted. A
// *url.Error (whose text includes the request URL) gets its URL replaced.
func scrubErr(err error) error {
	if err == nil {
		return nil
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue == err {
		return &url.Error{Op: ue.Op, URL: Redact(ue.URL), Err: scrubErr(ue.Err)}
	}
	msg := err.Error()
	if r := Redact(msg); r != msg {
		return &redactedError{msg: r, err: err}
	}
	return err
}

// RedactWriter wraps w (e.g. the log output) so every write is passed
// through Redact. Used as a last line of defence for log.Printf("%v", err).
func RedactWriter(w io.Writer) io.Writer { return redactWriter{w} }

type redactWriter struct{ w io.Writer }

func (r redactWriter) Write(p []byte) (int, error) {
	s := Redact(string(p))
	if _, err := io.WriteString(r.w, s); err != nil {
		return 0, err
	}
	return len(p), nil
}

func mask(s string) string {
	if s == "" {
		return ""
	}
	return "REDACTED"
}

// redacted is Creds with every secret masked (for printing only).
func (c Creds) redacted() any {
	type plain Creds // drops the methods so fmt doesn't recurse
	p := plain(c)
	p.Token, p.Cookie, p.CookieDS, p.EnterpriseToken = mask(p.Token), mask(p.Cookie), mask(p.CookieDS), mask(p.EnterpriseToken)
	return p
}

// String, GoString and Format keep tokens and cookies out of %v, %+v, %#v,
// %s and %q (including Creds nested in other structs, e.g. Client).
func (c Creds) String() string   { return fmt.Sprintf("%+v", c.redacted()) }
func (c Creds) GoString() string { return fmt.Sprintf("slack.Creds%+v", c.redacted()) }
func (c Creds) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		io.WriteString(f, c.GoString())
		return
	}
	io.WriteString(f, c.String())
}

func (in SignInInput) redacted() any {
	type plain SignInInput
	p := plain(in)
	p.Token, p.OrgToken, p.Cookie, p.CookieDS = mask(p.Token), mask(p.OrgToken), mask(p.Cookie), mask(p.CookieDS)
	return p
}

func (in SignInInput) String() string   { return fmt.Sprintf("%+v", in.redacted()) }
func (in SignInInput) GoString() string { return fmt.Sprintf("slack.SignInInput%+v", in.redacted()) }
func (in SignInInput) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		io.WriteString(f, in.GoString())
		return
	}
	io.WriteString(f, in.String())
}

// IsSlackHost reports whether host is slack.com or a subdomain of it, the
// only hosts that ever get the session cookie or a token. It is CookieHost
// (hosts.go): plain ASCII DNS names only, so look-alikes and IDN don't match.
func IsSlackHost(host string) bool { return CookieHost(host) }

// slackURL reports whether raw is an https URL on a Slack host (scheme
// "wss" for the realtime gateway): CookieURL with that exact scheme.
func slackURL(raw, scheme string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == scheme && cookieURL(u)
}
