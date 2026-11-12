package ui

import (
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/io/transfer"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/slack"
)

// Smart paste on the sign-in card. Ctrl/⌘+V anywhere on the card, the
// clipboard check when the window comes to the front (loginClipboard), and
// text pasted into any of the card's fields are all classified the same
// way (slack.ClassifyPaste) and land where they belong:
//
//   - a slack:// sign-in link signs in, as it always did;
//   - xoxd-…, d=xoxd-…, a Cookie header or a DevTools cookie row fills the
//     d cookie and signs in: Clack fetches the tokens (slack.SignIn);
//   - the console snippet's {teams:{…}} JSON fills Workspace, Token and
//     (Grid) Org token under "Use a token instead", picking the team that
//     matches Workspace;
//   - a bare xoxc-… token fills Token (Org token when that field has the
//     focus: the two look the same).
//
// A credential opens Advanced, ticks the field it filled and says so;
// once the d cookie is there the card signs in without a click. The
// clipboard is cleared once a credential is taken, as it is for a link.
// Anything else is left alone: no error, nothing kept, nothing logged
// (docs/SECURITY.md C-16).

// loginPasteMin is the fewest runes inserted at once that count as a paste
// in a field: typing a token by hand never reroutes it mid-word.
const loginPasteMin = 8

// loginTickFor is how long a filled field's tick stays.
const loginTickFor = 2600 * time.Millisecond

// loginPasteKeys reads Ctrl/⌘+V when no field has the focus (a focused
// field pastes into itself, and loginFieldPaste reads that) and the
// clipboard that answers it.
func (a *App) loginPasteKeys(gtx layout.Context) {
	l := a.login
	for {
		e, ok := gtx.Event(key.Filter{Name: "V", Required: key.ModShortcut})
		if !ok {
			break
		}
		if ke, ok := e.(key.Event); ok && ke.State == key.Press {
			gtx.Execute(clipboard.ReadCmd{Tag: &l.pasteTag})
		}
	}
	for {
		e, ok := gtx.Event(transfer.TargetFilter{Target: &l.pasteTag, Type: "application/text"})
		if !ok {
			break
		}
		if s, ok := readPaste(e); ok {
			a.loginTake(gtx, slack.ClassifyPaste(s, strings.TrimSpace(l.workspace.Text())), nil)
		}
	}
}

// readPaste is a clipboard DataEvent's text, at most slack.MaxPaste bytes.
func readPaste(e any) (string, bool) {
	de, ok := e.(transfer.DataEvent)
	if !ok {
		return "", false
	}
	rc := de.Open()
	b, err := io.ReadAll(io.LimitReader(rc, slack.MaxPaste+1))
	rc.Close()
	if err != nil || len(b) > slack.MaxPaste {
		return "", false
	}
	return string(b), true
}

// loginFieldPaste looks at what was just inserted into field ed (prev was
// its text before): a paste that belongs in another field moves there, and
// one that belongs here is cleaned up (a whole Cookie header in d cookie
// becomes the d value). It reports whether it took the text.
func (a *App) loginFieldPaste(gtx layout.Context, ed *widget.Editor) bool {
	l := a.login
	if l.prevText == nil {
		l.prevText = map[*widget.Editor]string{}
	}
	cur := ed.Text()
	prev := l.prevText[ed]
	if cur == prev {
		return false
	}
	l.prevText[ed] = cur
	ins := insertedSpan(prev, cur)
	if utf8.RuneCountInString(ins) < loginPasteMin {
		return false
	}
	p := slack.ClassifyPaste(ins, strings.TrimSpace(l.workspace.Text()))
	switch {
	case p.Kind == slack.PasteNone:
		return false
	case p.Kind == slack.PasteLink && ed == &l.link:
		return false // the link field's own: updateLogin signs in with it
	}
	// the text goes where it belongs; this field gets back what it had
	l.setField(ed, prev, false)
	return a.loginTake(gtx, p, ed)
}

// insertedSpan is what changed from prev to cur: cur minus the prefix and
// suffix they share.
func insertedSpan(prev, cur string) string {
	i := 0
	for i < len(prev) && i < len(cur) && prev[i] == cur[i] {
		i++
	}
	for i > 0 && !utf8.RuneStart(cur[i]) {
		i--
	}
	j := 0
	for j < len(prev)-i && j < len(cur)-i && prev[len(prev)-1-j] == cur[len(cur)-1-j] {
		j++
	}
	for j > 0 && !utf8.RuneStart(cur[len(cur)-j]) {
		j--
	}
	return cur[i : len(cur)-j]
}

// setField puts v in ed (caret at its end); tick marks it filled.
func (l *loginScreen) setField(ed *widget.Editor, v string, tick bool) {
	if l.prevText == nil {
		l.prevText = map[*widget.Editor]string{}
	}
	ed.SetText(v)
	n := ed.Len()
	ed.SetCaret(n, n)
	l.prevText[ed] = ed.Text()
	if tick {
		if l.filled == nil {
			l.filled = map[*widget.Editor]time.Time{}
		}
		l.filled[ed] = l.now
	}
}

// loginTake applies a classified paste (from: the field it was pasted
// into, nil for the clipboard). It reports whether it took it.
func (a *App) loginTake(gtx layout.Context, p slack.Paste, from *widget.Editor) bool {
	l := a.login
	if l == nil || l.busy {
		return false
	}
	var what []string
	switch p.Kind {
	case slack.PasteLink:
		loginWriteClipboard(gtx, "") // a one-time secret
		a.signInWithLinkNow(p.Link)
		return true
	case slack.PasteTeams:
		if ws := teamHost(p.Workspace); ws != "" {
			l.setField(&l.workspace, ws, true)
			what = append(what, "workspace")
		}
		l.setField(&l.token, p.Token, true)
		what = append(what, "token")
		if p.OrgToken != "" {
			l.setField(&l.orgToken, p.OrgToken, true)
			l.orgURL = p.OrgURL
			what = append(what, "org token")
		}
	case slack.PasteToken:
		if from == &l.orgToken {
			l.setField(&l.orgToken, p.Token, true)
			what = append(what, "org token")
		} else {
			l.setField(&l.token, p.Token, true)
			what = append(what, "token")
		}
	case slack.PasteCookie:
		l.setField(&l.cookie, p.Cookie, true)
		what = append(what, "d cookie")
	default:
		return false
	}
	loginWriteClipboard(gtx, "") // a credential: off the clipboard
	a.introToSignIn()            // the introduction steps aside for the card
	if !l.advanced {
		l.advanced, l.trouble = true, false
	}
	if p.Kind != slack.PasteCookie {
		l.tokenForm = true // a token goes in the token fields
	}
	l.err = ""
	l.pasteNote = "Filled the " + joinAnd(what) + "."
	l.pasteAt = l.now
	// the focus goes to the d cookie, the one thing still missing
	l.focusTo, l.focused = &l.cookie, false
	if strings.TrimSpace(l.cookie.Text()) != "" {
		a.loginSubmitTokens() // d is there: no click needed
	}
	return true
}

// teamHost is a team URL's host (https://hackclub.slack.com/ →
// hackclub.slack.com), what the Workspace field shows.
func teamHost(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return u
}

// joinAnd is "a", "a and b", "a, b and c".
func joinAnd(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// loginSubmitTokens signs in with Advanced's fields, or says what's
// missing. The d cookie is enough (slack.SignIn fetches the tokens); a
// token, with "Use a token instead" open, is used as it is.
func (a *App) loginSubmitTokens() {
	l := a.login
	in := slack.SignInInput{
		Workspace: strings.TrimSpace(l.workspace.Text()),
		Cookie:    strings.TrimSpace(l.cookie.Text()),
	}
	if l.tokenForm {
		in.Token = strings.TrimSpace(l.token.Text())
		in.OrgToken = strings.TrimSpace(l.orgToken.Text())
		if in.OrgToken != "" {
			in.OrgURL = l.orgURL // from the pasted JSON (there's no field for it)
		}
	}
	switch {
	case in.Cookie == "":
		l.err = "Paste the d cookie first."
	case in.Token == "" && in.OrgToken != "":
		l.err = "Paste the workspace token too."
	default:
		a.startLogin(in)
	}
}

// loginTick is how far field ed's tick is in (0 none … 1 shown, past 1 for
// a moment as it pops) and its fade.
func (l *loginScreen) loginTick(ed *widget.Editor) (pop, alpha float32) {
	at, ok := l.filled[ed]
	if !ok {
		return 0, 0
	}
	t := l.now.Sub(at)
	if t >= loginTickFor {
		delete(l.filled, ed)
		return 0, 0
	}
	if l.mc != nil {
		l.mc.moving = true
	}
	alpha = 1
	if fade := loginTickFor - 400*time.Millisecond; t > fade {
		alpha = 1 - float32(t-fade)/float32(400*time.Millisecond)
	}
	pop = 1
	if !l.reduced && l.mc != nil {
		pop = float32(l.mc.spring(springPop, at))
	}
	return pop, alpha
}
