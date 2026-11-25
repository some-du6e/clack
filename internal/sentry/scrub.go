package sentry

import (
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/applog"
)

// Scrub is what every string an event carries goes through before it is
// sent:
//
//   - applog.Redact, as for every line of clack.log: xox* tokens (plain
//     and URL-encoded), the d, d-s and lc cookies, token= and the other
//     secrets in query strings, Cookie, Set-Cookie and Authorization
//     values, Bearer credentials, secrets in JSON, the query of a
//     slack:// link;
//   - the user's home directory, however a path spells it (/ or \, and
//     in any case on Windows and macOS, whose file systems ignore it), as
//     ~, and any other home (/home/<name>, /Users/<name>,
//     C:\Users\<name>) as <home>/<user>.
//
// The report box's comment and rows and a crash's stack go through it;
// a crash's message (scrubCrashText) also loses what Go quotes in it.
func Scrub(s string) string {
	if s == "" {
		return s
	}
	s = applog.Redact(s)
	if re := homePattern(); re != nil {
		s = re.ReplaceAllString(s, "~${1}")
	}
	return otherHomeRE.ReplaceAllString(s, "${1}<user>")
}

// homeDir is the home directory Scrub replaces (tests set it).
var homeDir = func() string {
	h, _ := os.UserHomeDir()
	return h
}

// homeCache is homePattern's, for the home it was built for.
var homeCache struct {
	sync.Mutex
	home string
	re   *regexp.Regexp
}

// homePattern matches the user's home directory as a path may spell it:
// either separator, and in any case where the file system ignores case.
func homePattern() *regexp.Regexp {
	h := strings.TrimRight(homeDir(), `/\`)
	homeCache.Lock()
	defer homeCache.Unlock()
	if h == homeCache.home {
		return homeCache.re
	}
	homeCache.home, homeCache.re = h, nil
	if len(h) < 2 {
		return nil
	}
	var b strings.Builder
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		b.WriteString("(?i)")
	}
	for _, r := range h {
		if r == '/' || r == '\\' {
			b.WriteString(`[/\\]`)
			continue
		}
		b.WriteString(regexp.QuoteMeta(string(r)))
	}
	// the whole name: /home/kar isn't /home/karim's (what follows it is
	// kept: ~${1})
	b.WriteString(`([/\\]|$|[^\w.\-])`)
	if re, err := regexp.Compile(b.String()); err == nil {
		homeCache.re = re
	}
	return homeCache.re
}

// otherHomeRE is a home directory's user part: /home/<name>,
// /Users/<name>, C:\Users\<name> (either separator, any case), where an
// absolute path starts (a module path's own .../home/... isn't one).
var otherHomeRE = regexp.MustCompile(`(?im)((?:^|[\s"'(=\[])(?:[a-z]:)?[/\\](?:users|home)[/\\])[^/\\\s"'<>:;,|)]+`)

// The parts of a crash's message that are data, not code: what Go quotes
// (%q, backquotes, a quoted run of more than one character), Slack's ids
// and message timestamps, and email addresses.
var (
	quotedRE = regexp.MustCompile("\"(?:[^\"\\\\\\n]|\\\\.)*\"|`[^`\\n]*`|'[^'\\n]{2,}'")
	idRE     = regexp.MustCompile(`\b[UWCDGBTEFS][A-Z0-9]{8,}\b`)
	tsRE     = regexp.MustCompile(`\b\d{10}\.\d{6}\b`)
	emailRE  = regexp.MustCompile(`[\w.+\-]+@[\w\-]+(?:\.[\w\-]+)+`)
)

// maxCrashText is the most of a crash's message an event keeps.
const maxCrashText = 300

// scrubCrashText is Scrub for a crash's message and its records (a
// panic's value, a native exception's line, the exit reason), which can
// quote data the code was handling when it crashed: a quoted value
// becomes "…", an id <id>, a message timestamp <ts>, an email <email>,
// and it is cut at maxCrashText characters.
func scrubCrashText(s string) string {
	s = Scrub(s)
	s = quotedRE.ReplaceAllStringFunc(s, func(m string) string {
		q := m[:1]
		return q + "…" + q
	})
	s = emailRE.ReplaceAllString(s, "<email>")
	s = idRE.ReplaceAllString(s, "<id>")
	s = tsRE.ReplaceAllString(s, "<ts>")
	if r := []rune(s); len(r) > maxCrashText {
		s = string(r[:maxCrashText-1]) + "…"
	}
	return s
}
