package applog

import (
	"regexp"

	"github.com/some-du6e/clack/internal/slack"
)

// Redact is what every line written to the log file goes through:
// slack.Redact (xox* tokens, the d, d-s and lc cookies, token=,
// enterprise_token=, magic_tokens= and tokens= values, magic-login link
// tokens, the Slack CLI's "/slackauthticket <ticket>" and its ticket= and
// challenge=), then the rules below for what the file keeps longer than
// stderr does:
//
//   - URL-encoded tokens: xoxc%2D…
//   - Cookie:, Set-Cookie: and Authorization: values (to the end of the
//     line), where the name is a header: at the start of a line, or after
//     punctuation (as in a Go http.Header "map[Authorization:[…] Cookie:[…]]"
//     or "dial: Cookie: …"), or written the way a header is after a space
//     ("sent Authorization: …"); a lowercase name after a word is prose
//     ("notification authorization: granted") and stays
//   - Bearer <credential>
//   - JSON "token", "cookie", "d", "password", "secret", "access_token"
//     and "refresh_token" string values
//   - slack:// links: everything after "?" (a deep link can name what
//     was searched for or opened)
func Redact(s string) string {
	s = slack.Redact(s)
	s = encodedTokenRE.ReplaceAllStringFunc(s, func(m string) string { return m[:4] + "-REDACTED" })
	s = headerRE.ReplaceAllString(s, "${1}REDACTED")
	s = bearerRE.ReplaceAllString(s, "${1}REDACTED")
	s = jsonSecretRE.ReplaceAllString(s, `${1}"REDACTED"`)
	s = deepLinkQueryRE.ReplaceAllString(s, "${1}?…")
	return s
}

var (
	encodedTokenRE  = regexp.MustCompile(`(?i)xox[a-z]%2d[^\s"'&;,)<>\]]+`)
	headerRE        = regexp.MustCompile(`(?im)((?:(?:^|[\[\](){}<>"'\x60,;:=|])[ \t]*(?:cookie|set-cookie|authorization)|\s(?-i:Cookie|Set-Cookie|Authorization|COOKIE|SET-COOKIE|AUTHORIZATION))[ \t]*:[ \t]*)[^\n]+`)
	bearerRE        = regexp.MustCompile(`(?i)\b(bearer\s+)[^\s"',;]+`)
	jsonSecretRE    = regexp.MustCompile(`(?i)("(?:token|cookie|d|password|secret|access_token|refresh_token)"\s*:\s*)"[^"]*"`)
	deepLinkQueryRE = regexp.MustCompile(`(?i)(slack://[^\s?"'<>]*)\?[^\s"'<>]*`)
)
