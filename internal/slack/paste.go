package slack

import (
	"strings"
)

// PasteKind is what a pasted text (or the clipboard) holds, for the
// sign-in card's smart paste.
type PasteKind int

const (
	PasteNone   PasteKind = iota // anything else: left alone
	PasteLink                    // a slack:// magic-login or login-v2 link
	PasteTeams                   // the Advanced snippet's {teams:{…}} JSON
	PasteToken                   // a bare xoxc-… token
	PasteCookie                  // xoxd-…, d=xoxd-…, a Cookie header or a DevTools cookie row
)

// Paste is a classified paste. Only the fields of its kind are set.
type Paste struct {
	Kind      PasteKind
	Link      string // PasteLink
	Workspace string // PasteTeams: the matching team's URL
	Token     string // PasteTeams, PasteToken: xoxc-…
	OrgToken  string // PasteTeams: the org's (E…) token, if any
	OrgURL    string // PasteTeams: the org's https://….enterprise.slack.com/
	Cookie    string // PasteCookie: the d cookie's value (as pasted, not re-encoded)
}

// MaxPaste is the longest text ClassifyPaste reads: the teams JSON of a
// large org is tens of KB; anything longer isn't a credential.
const MaxPaste = 512 << 10

// ClassifyPaste says what s is. It is untrusted input (the clipboard, a
// paste): every kind is checked for its exact shape, and anything that
// doesn't fit is PasteNone, never an error. workspace picks the team out
// of a teams JSON, as SignIn does. Nothing is logged.
func ClassifyPaste(s, workspace string) Paste {
	if len(s) > MaxPaste {
		return Paste{}
	}
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return Paste{}
	case IsMagicLink(s):
		return Paste{Kind: PasteLink, Link: s}
	case strings.HasPrefix(s, "{") || strings.HasPrefix(s, "["):
		in := SignInInput{Token: s, Workspace: workspace}
		if expandPastedTeams(&in) != nil || !validToken(in.Token) {
			return Paste{}
		}
		p := Paste{Kind: PasteTeams, Workspace: in.Workspace, Token: in.Token}
		if validToken(in.OrgToken) && slackURL(in.OrgURL, "https") {
			p.OrgToken, p.OrgURL = in.OrgToken, in.OrgURL
		}
		return p
	case validToken(s):
		return Paste{Kind: PasteToken, Token: s}
	}
	if v := pastedDCookie(s); v != "" {
		return Paste{Kind: PasteCookie, Cookie: v}
	}
	return Paste{}
}

// validToken: xoxc- and then Slack's token characters (digits, letters,
// dashes), 20 to 512 bytes.
func validToken(s string) bool {
	if !strings.HasPrefix(s, "xoxc-") || len(s) < 20 || len(s) > 512 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
			return false
		}
	}
	return true
}

// pastedDCookie finds the d cookie's value in s: a bare xoxd-… value, or
// one named d in "d=…; d-s=…" (a Cookie header), or the Name and Value
// cells of a DevTools cookie row (tab-separated). It returns "" unless
// the value is xoxd- (or its URL-encoded xoxd%…) and nothing but cookie
// characters. A d-s value never matches (it is digits).
func pastedDCookie(s string) string {
	if len(s) > 8<<10 {
		return ""
	}
	var cand string
	switch {
	case strings.HasPrefix(s, "xoxd-") || strings.HasPrefix(s, "xoxd%"):
		cand = s
	default:
		// a Cookie header: name=value pairs split by ;
		for _, part := range strings.Split(s, ";") {
			name, val, ok := strings.Cut(strings.TrimSpace(part), "=")
			if ok && strings.TrimSpace(name) == "d" {
				cand = strings.TrimSpace(val)
				break
			}
		}
		if cand == "" {
			// a DevTools row: "d<TAB>xoxd-…<TAB>.slack.com<TAB>/…"
			if f := strings.Split(s, "\t"); len(f) >= 2 && strings.TrimSpace(f[0]) == "d" {
				cand = strings.TrimSpace(f[1])
			}
		}
	}
	if i := strings.IndexAny(cand, " \t\r\n;"); i >= 0 {
		cand = cand[:i]
	}
	if !strings.HasPrefix(cand, "xoxd-") && !strings.HasPrefix(cand, "xoxd%") {
		return ""
	}
	if len(cand) < 20 || len(cand) > 4096 {
		return ""
	}
	for _, r := range cand {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || strings.ContainsRune("-%._~+/=", r)) {
			return ""
		}
	}
	return cand
}
