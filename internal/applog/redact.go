package applog

import (
	"regexp"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Redact is what every line written to the log file goes through:
// slack.Redact (xox* tokens, the d, d-s and lc cookies, token=,
// enterprise_token=, magic_tokens= and tokens= values, magic-login link
// tokens), then the rules below for what the file keeps longer than
// stderr does:
//
//   - URL-encoded tokens: xoxc%2D…
//   - Cookie:, Set-Cookie: and Authorization: values (to the end of the line)
//   - Bearer <credential>
//   - JSON "token", "cookie", "d", "password", "secret", "access_token"
//     and "refresh_token" string values
//   - slack:// links: everything after "?" (a deep link can name what
//     was searched for or opened)
func Redact(s string) string {
	s = slack.Redact(s)
	s = encodedTokenRE.ReplaceAllStringFunc(s, func(m string) string { return m[:4] + "-REDACTED" })
	s = headerRE.ReplaceAllString(s, "${1}${2}REDACTED")
	s = bearerRE.ReplaceAllString(s, "${1}REDACTED")
	s = jsonSecretRE.ReplaceAllString(s, `${1}"REDACTED"`)
	s = deepLinkQueryRE.ReplaceAllString(s, "${1}?…")
	return s
}

var (
	encodedTokenRE  = regexp.MustCompile(`(?i)xox[a-z]%2d[^\s"'&;,)<>\]]+`)
	headerRE        = regexp.MustCompile(`(?i)\b(cookie|set-cookie|authorization)(\s*:\s*)[^\n]+`)
	bearerRE        = regexp.MustCompile(`(?i)\b(bearer\s+)[^\s"',;]+`)
	jsonSecretRE    = regexp.MustCompile(`(?i)("(?:token|cookie|d|password|secret|access_token|refresh_token)"\s*:\s*)"[^"]*"`)
	deepLinkQueryRE = regexp.MustCompile(`(?i)(slack://[^\s?"'<>]*)\?[^\s"'<>]*`)
)
