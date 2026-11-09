package popups

import (
	"net/url"
	"regexp"
	"strings"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/openurl"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Slack's suspicious-link warning (boot-shared 8b7U isLinkSuspicious and
// IVSi SuspiciousLinkWarning, build a84662d2d135): clicking a link whose
// text names a different place than its URL opens "Double-check this
// link" unless users.prefs suppress_link_warning is set (Advanced >
// "Warn me about potentially malicious links" is its negation). The
// dialog's "Don’t show this again" sets suppress_link_warning=true when
// the user continues.

// linkConfusables are the look-alike characters that make a link's text
// suspicious on their own.
var linkConfusables = regexp.MustCompile("[\u180E\u2009\u200A\u200B\u202F\uFEFF\u2215\u2044\u02D0\u02F8\uA789\uFE13\uFE55\uA4F8\u0589\u06D4\uFE52\u00AD\u200E\u200F\u202A\u202B\u202D\u202E\u2AFD\u2571\u27CB\u4E3F\uA4FD\u29F8\u0660\u2024\u1735\uFE30\U0001D601\U0001D429\U0001D554\U0001D70C\U0001D4FC\u0665\U0001D4F7\u210E\U0001D70F\U0001D7BD\U0001D531\U0001D484\U0001D604\U0001D595\u0455\U0001D421\U0001D669\U0001D5FD\U0001D6D5\U0001D600\U0001D691\U0001D598\U0001D599\U0001D780\u2CA5\u1D11\U0001D6D4\u0441\U0001D6D0\U0001D520\U0001D594\U0001D68C\U0001D450\u1040\U0001D588\U0001D498]")

// linkInText is the first URL-like run in a link's text (linkify with
// every TLD): a scheme URL or a bare domain, not an email's domain.
var linkInText = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^\s<>]+|(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}(?::\d+)?(?:/[^\s<>]*)?)`)

var slackSchemes = []string{"slackbeta:", "slackprototype:", "slackmdm:", "slackdebug:", "slack:"}

func slackScheme(u *url.URL) bool {
	for _, s := range slackSchemes {
		if u.Scheme+":" == s {
			return true
		}
	}
	return false
}

func slackHost(h string) bool {
	h = strings.ToLower(h)
	return h == "slack.com" || strings.HasSuffix(h, ".slack.com")
}

func linkHost(raw string) (*url.URL, string) {
	if !strings.Contains(raw, "://") && !strings.HasPrefix(strings.ToLower(raw), "mailto:") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return &url.URL{}, ""
	}
	return u, strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// LinkSuspicious is isLinkSuspicious(text, url).
func LinkSuspicious(text, href string) bool {
	if text == "" {
		return false
	}
	if text == href {
		if strings.Contains(href, "@") { // a URL hidden after "@" in the path
			u, _ := linkHost(href)
			if i := strings.Index(u.Path, "@"); i >= 0 {
				if m := linkInText.FindStringIndex(u.Path[i+1:]); m != nil && m[0] == 0 {
					return true
				}
			}
		}
		return false
	}
	t := strings.TrimSpace(text)
	if t == "" || linkConfusables.MatchString(t) {
		return true
	}
	if strings.ContainsAny(t[:1], "#@") || strings.HasPrefix(t, "＃") || strings.HasPrefix(t, "＠") {
		u, h := linkHost(href)
		return !(slackHost(h) || slackScheme(u))
	}
	t = strings.NewReplacer("⧸", "/", "：", ":", "．", ".").Replace(t)
	for _, m := range linkInText.FindAllStringIndex(t, -1) {
		if m[0] > 0 && t[m[0]-1] == '@' {
			continue // an email address, not a link
		}
		_, th := linkHost(t[m[0]:m[1]])
		u, h := linkHost(href)
		if slackScheme(u) {
			return false
		}
		return th != h
	}
	return false
}

// ConfirmLink is SuspiciousLinkWarning: "Double-check this link", the
// text (25 runes) and URL (50 runes) it names, "Don’t show this again"
// and Cancel / Continue (danger), which opens the URL.
func ConfirmLink(h Host, text, href string, setSuppress func()) *Modal {
	th := h.Theme()
	short := func(s string, n int) string {
		if r := []rune(s); len(r) > n {
			return string(r[:n]) + "…"
		}
		return s
	}
	var dontShow bool
	var box widget.Clickable
	m := NewModal(h, ModalOpts{
		Title:        "Double-check this link",
		EnterSubmits: true,
		Body: func(gtx layout.Context) layout.Dimensions {
			cl := colorsOf(th)
			if box.Clicked(gtx) {
				dontShow = !dontShow
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return para(gtx, th, 15, 22, theme.Regular, cl.Fg, "The link "+short(text, 25)+" is taking you to another site: "+short(href, 50)+". Are you sure you want to continue?", 0)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 16}.Layout(gtx, prefCheckRow(h, &box, "Don’t show this again", func() bool { return dontShow }))
				}),
			)
		},
		Buttons: []*ModalButton{
			{Label: "Cancel", Kind: BtnOutline},
			{Label: "Continue", Kind: BtnDanger, Do: func() bool {
				if dontShow && setSuppress != nil {
					setSuppress()
				}
				if err := openurl.Open(href); err != nil {
					h.Toast("Couldn't open the link: " + err.Error())
				}
				return true
			}},
		},
	})
	h.ShowOverlay(m)
	return m
}

// MaybeWarnLink is onMaybeOpenSuspiciousLinkDialog: with
// suppress_link_warning unset and a suspicious link, the dialog opens and
// it reports true (the click is handled).
func MaybeWarnLink(h Host, text, href string) bool {
	c := h.Core()
	if c == nil || c.PrefBool("suppress_link_warning", false) || !LinkSuspicious(text, href) {
		return false
	}
	ConfirmLink(h, text, href, func() { setPrefAsync(h, "suppress_link_warning", true) })
	return true
}
