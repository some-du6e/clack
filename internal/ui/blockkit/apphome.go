package blockkit

import (
	"context"
	"image"
	"sort"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// AppDM is an app's DM: Slack shows its tabs (Home when the app has an
// App Home, Messages, About) instead of the channel tab bar.
//
// Slack opens it with conversations.info {return_app_home: true} (tab
// flags + the published home view), tells the app which tab is open
// with apps.home.dispatchOpenEvent (the app usually answers with a fresh
// views.publish, which arrives on RTM as view_opened type home), and
// fills About from apps.profile.get.
type AppDM struct {
	h       Host
	ch      string
	user    core.User
	home    core.AppHome
	known   bool // app_home loaded
	tab     string
	tabs    [3]tap
	surf    *Surface
	list    layout.List
	about   *core.AppProfile
	aboutV  *richview.View
	loading bool
	opened  map[string]bool
}

var (
	dmsMu sync.Mutex
	dms   = map[string]*AppDM{}
	notDM = map[string]bool{} // conversations known not to be app DMs
)

// AppDMFor returns the app DM state for conversation ch, or nil when ch
// isn't a DM with an app (or isn't known to be one yet).
func AppDMFor(h Host, ch string) *AppDM {
	c := h.Core()
	if c == nil {
		return nil
	}
	dmsMu.Lock()
	d, no := dms[ch], notDM[ch]
	dmsMu.Unlock()
	if d != nil {
		return d.ready()
	}
	if no {
		return nil
	}
	cv, ok := c.Conv(ch)
	if !ok {
		return nil
	}
	if cv.Kind != "im" || cv.User == "" || cv.User == "USLACKBOT" {
		dmsMu.Lock()
		notDM[ch] = true
		dmsMu.Unlock()
		return nil
	}
	u, ok := c.User(cv.User)
	if !ok {
		return nil
	}
	if !u.IsBot {
		dmsMu.Lock()
		notDM[ch] = true
		dmsMu.Unlock()
		return nil
	}
	d = &AppDM{h: h, ch: ch, user: u, tab: "messages", opened: map[string]bool{}}
	d.list.Axis = layout.Vertical
	dmsMu.Lock()
	dms[ch] = d
	dmsMu.Unlock()
	d.load()
	return d.ready()
}

func (d *AppDM) ready() *AppDM {
	if !d.known {
		return nil
	}
	return d
}

func (d *AppDM) load() {
	if d.loading {
		return
	}
	d.loading = true
	ch := d.ch
	run(d.h, 15*time.Second, func(ctx context.Context) (struct {
		h core.AppHome
		v *core.View
	}, error) {
		var r struct {
			h core.AppHome
			v *core.View
		}
		var err error
		r.h, r.v, err = d.h.Core().Interactive().AppHome(ctx, ch)
		return r, err
	}, func(r struct {
		h core.AppHome
		v *core.View
	}, err error) {
		d.loading = false
		if err != nil || r.h.AppID == "" {
			return
		}
		d.home, d.known = r.h, true
		if r.v != nil {
			d.surf = newSurface(d.h, r.v)
		}
		// the tab Slack opens from the sidebar (no tab asked for)
		d.tab = initialTab("", d.home.MessagesTab, d.home.HomeTab)
		d.dispatchOpen(d.tab)
		d.h.Invalidate()
	})
}

// homeUpdated applies a realtime home view.
func homeUpdated(ch string, v *core.View) {
	dmsMu.Lock()
	d := dms[ch]
	dmsMu.Unlock()
	if d == nil || v == nil {
		return
	}
	if d.surf != nil && d.surf.view.ID == v.ID {
		d.surf.set(v, true)
	} else {
		d.surf = newSurface(d.h, v)
	}
	d.home.HomeTab = true
	d.h.Invalidate()
}

// initialTab is Slack's app tab resolver (module HKzj): the tab asked
// for ("home", "about", "" / "messages"), falling back to Home or About
// when the Messages tab is off, and to Messages or About when Home is.
// A sidebar click asks for none; deep links from an app id ask for Home.
func initialTab(want string, messages, home bool) string {
	n := ""
	switch want {
	case "home", "app":
		n = "home"
	case "about":
		n = "about"
	}
	if !messages && n == "" {
		if home {
			return "home"
		}
		return "about"
	}
	if !home && n == "home" {
		if messages {
			return "messages"
		}
		return "about"
	}
	if n == "" {
		return "messages"
	}
	return n
}

// OpenTab opens the tab a deep link asks for (Slack's
// deepLinkFromAppIdToApp: "home" unless the link names one).
func (d *AppDM) OpenTab(want string) { d.SetTab(initialTab(want, d.home.MessagesTab, d.home.HomeTab)) }

// Tabs lists the DM's tabs in Slack's order: Home and Messages when the
// app enables them, then About.
func (d *AppDM) Tabs() []string {
	var out []string
	if d.home.HomeTab {
		out = append(out, "home")
	}
	if d.home.MessagesTab {
		out = append(out, "messages")
	}
	return append(out, "about")
}

// Tab is the open tab: "home", "messages" or "about".
func (d *AppDM) Tab() string { return d.tab }

// ReadOnly reports whether the Messages tab is read-only (Slack replaces
// the composer with "Sending messages to this app has been turned off.").
func (d *AppDM) ReadOnly() bool { return d.home.MessagesTab && d.home.MessagesRO }

// SetTab opens a tab (and tells the app, like Slack).
func (d *AppDM) SetTab(t string) {
	if d.tab == t {
		return
	}
	d.tab = t
	d.dispatchOpen(t)
	d.h.Invalidate()
}

// dispatchOpen tells the app a tab opened (apps.home.dispatchOpenEvent:
// the app gets app_home_opened) or loads About.
func (d *AppDM) dispatchOpen(t string) {
	switch t {
	case "home", "messages":
		ch, tab, team := d.ch, t, d.home.AppInstalledTeamID
		ix := d.h.Core().Interactive()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = ix.HomeOpened(ctx, ch, tab, team)
		}()
	case "about":
		d.loadAbout()
	}
}

var tabLabel = map[string]string{"home": "Home", "messages": "Messages", "about": "About"}
var tabIcon = map[string]string{"home": "home", "messages": "threads", "about": "info"}

// LayoutTabs draws the tab bar (Slack's channel tabs look: 13/700,
// 16px icon, 2px aubergine underline on the open tab).
func (d *AppDM) LayoutTabs(gtx C) D {
	drain()
	th := d.h.Theme()
	cl := colorsOf(th)
	sz := gtx.Constraints.Max
	x := dp(gtx, 16)
	for i, t := range d.Tabs() {
		if i >= len(d.tabs) {
			break
		}
		tp := &d.tabs[i]
		if tp.clicked(gtx, d.h) {
			d.SetTab(t)
		}
		lbl := tabLabel[t]
		lw := measure(gtx, th, 13, theme.Bold, lbl)
		w := dp(gtx, 8) + dp(gtx, 16) + dp(gtx, 4) + lw + dp(gtx, 8)
		cur := t == d.tab
		at(gtx, image.Pt(x, 0), func(gtx C) D {
			if tp.g.Hovered() && !cur {
				rrect(gtx, image.Rect(0, dp(gtx, 5), w, sz.Y-dp(gtx, 5)), dp(gtx, 6), th.C.BgHover)
			}
			if cur {
				rrect(gtx, image.Rect(0, sz.Y-dp(gtx, 2), w, sz.Y), 0, th.C.Accent)
			}
			col := th.C.FgMuted
			if cur {
				col = th.C.Fg
			}
			at(gtx, image.Pt(dp(gtx, 8), (sz.Y-dp(gtx, 16))/2), func(gtx C) D { return icon(gtx, tabIcon[t], 16, col) })
			at(gtx, image.Pt(dp(gtx, 28), 0), func(gtx C) D {
				return vcenter(gtx, sz.Y, func(gtx C) D { return label(gtx, th, 13, theme.Bold, col, lbl) })
			})
			tp.add(gtx, image.Pt(w, sz.Y))
			return D{}
		})
		x += w + dp(gtx, 4)
	}
	_ = cl
	return D{Size: sz}
}

// LayoutBody draws the Home or About tab (Messages is the ui root's
// conversation view).
func (d *AppDM) LayoutBody(gtx C) D {
	drain()
	sz := gtx.Constraints.Max
	th := d.h.Theme()
	rrect(gtx, image.Rectangle{Max: sz}, 0, th.C.Bg)
	gtx.Constraints = layout.Exact(sz)
	switch d.tab {
	case "home":
		if d.surf == nil {
			return d.empty(gtx, "This app hasn't set up its Home tab yet.")
		}
		return d.list.Layout(gtx, 1, func(gtx C, _ int) D {
			w := min(sz.X-dp(gtx, 40), dp(gtx, 1000))
			// Slack: blocks at (24, 31) under the tabs, 969 wide at 1440 (45 right).
			return layout.Inset{Left: unit.Dp(24), Right: unit.Dp(45), Top: unit.Dp(31), Bottom: unit.Dp(24)}.Layout(gtx, func(gtx C) D {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
				return d.surf.Layout(gtx)
			})
		})
	case "about":
		return d.list.Layout(gtx, 1, func(gtx C, _ int) D { return d.layoutAbout(gtx, sz.X) })
	}
	return D{Size: sz}
}

func (d *AppDM) empty(gtx C, msg string) D {
	th := d.h.Theme()
	cl := colorsOf(th)
	sz := gtx.Constraints.Max
	w := measure(gtx, th, 15, theme.Regular, msg)
	at(gtx, image.Pt((sz.X-w)/2, sz.Y/3), func(gtx C) D { return label(gtx, th, 15, theme.Regular, cl.Text2, msg) })
	return D{Size: sz}
}

func (d *AppDM) loadAbout() {
	if d.about != nil {
		return
	}
	bot, app := "", d.home.AppID
	if c := d.h.Core(); c != nil {
		if u, ok := c.User(d.user.ID); ok {
			d.user = u
		}
	}
	ix := d.h.Core().Interactive()
	run(d.h, 15*time.Second, func(ctx context.Context) (core.AppProfile, error) {
		return ix.AppProfileOf(ctx, bot, app)
	}, func(p core.AppProfile, err error) {
		if err != nil {
			p = core.AppProfile{Name: firstNonEmpty(d.user.RealName, d.user.Name)}
		}
		sort.Slice(p.Commands, func(i, j int) bool { return p.Commands[i].Name < p.Commands[j].Name })
		d.about = &p
		if p.LongDesc != "" {
			d.aboutV = richview.New(richtext.Doc{Blocks: d.h.Core().Mrkdwn(p.LongDesc)}, richview.Options{})
		}
		d.h.Invalidate()
	})
}

// layoutAbout: the app's icon, name with the APP tag, description,
// long description, slash commands and links.
func (d *AppDM) layoutAbout(gtx C, w int) D {
	th := d.h.Theme()
	cl := colorsOf(th)
	p := d.about
	x := dp(gtx, 20)
	cw := min(w-2*x, dp(gtx, 720))
	y := dp(gtx, 24)
	name := firstNonEmpty(d.user.RealName, d.user.Name)
	desc, icon := "", d.user.Avatar192
	if p != nil {
		name = firstNonEmpty(p.Name, name)
		desc = p.Desc
		icon = firstNonEmpty(p.Icon, icon)
	}
	is := dp(gtx, 72)
	if img, ok := d.h.Images().Get(icon, is); ok {
		at(gtx, image.Pt(x, y), func(gtx C) D { return theme.Image(gtx, img, image.Pt(is, is), dp(gtx, 12)) })
	} else {
		at(gtx, image.Pt(x, y), func(gtx C) D { return th.Placeholder(gtx, image.Pt(is, is), dp(gtx, 12)) })
	}
	tx := x + is + dp(gtx, 16)
	at(gtx, image.Pt(tx, y+dp(gtx, 8)), func(gtx C) D {
		dd := label(gtx, th, 22, theme.Black, cl.Text, name)
		at(gtx, image.Pt(dd.Size.X+dp(gtx, 8), dp(gtx, 6)), func(gtx C) D { return popups.Tag(gtx, th, "APP", false, false) })
		return dd
	})
	if desc != "" {
		at(gtx, image.Pt(tx, y+dp(gtx, 42)), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw-is-dp(gtx, 16), dp(gtx, 44))}
			return para(gtx, th, 15, 22, theme.Regular, cl.Text2, desc)
		})
	}
	y += is + dp(gtx, 24)
	if p == nil {
		s := dp(gtx, 24)
		at(gtx, image.Pt(x, y), func(gtx C) D { spinner(gtx, s, cl.Text2); return D{} })
		return D{Size: image.Pt(w, y+s+dp(gtx, 24))}
	}
	section := func(title string) {
		at(gtx, image.Pt(x, y), func(gtx C) D { return label(gtx, th, 15, theme.Bold, cl.Text, title) })
		y += dp(gtx, 28)
	}
	if d.aboutV != nil {
		section("Description")
		dd := at(gtx, image.Pt(x, y), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return d.aboutV.Layout(gtx, styleFor(th, d.h.Images()), &surfHandler{h: d.h})
		})
		y += dd.Size.Y + dp(gtx, 20)
	}
	if len(p.Commands) > 0 {
		section("Slash commands")
		for _, c := range p.Commands {
			at(gtx, image.Pt(x, y), func(gtx C) D {
				dd := label(gtx, th, 15, theme.Bold, cl.Text, c.Name+" "+c.Usage)
				if c.Desc != "" {
					at(gtx, image.Pt(0, dp(gtx, 22)), func(gtx C) D { return label(gtx, th, 13, theme.Regular, cl.Text2, c.Desc) })
				}
				return dd
			})
			y += dp(gtx, 44)
		}
		y += dp(gtx, 8)
	}
	links := [][2]string{{"Website", p.URL}, {"Support", p.SupportURL}, {"Privacy policy", p.PrivacyURL}}
	for _, l := range links {
		if l[1] == "" {
			continue
		}
		l := l
		at(gtx, image.Pt(x, y), func(gtx C) D {
			t := linkTaps[l[1]]
			if t == nil {
				t = &tap{}
				linkTaps[l[1]] = t
			}
			if t.clicked(gtx, d.h) {
				openURL(l[1])
			}
			dd := label(gtx, th, 15, theme.Regular, cl.Link, l[0])
			t.add(gtx, dd.Size)
			return dd
		})
		y += dp(gtx, 26)
	}
	return D{Size: image.Pt(w, y+dp(gtx, 24))}
}

var linkTaps = map[string]*tap{}
