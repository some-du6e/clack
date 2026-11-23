package popups

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"
	"time"
	"unicode"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Add emoji dialog's Emoji packs tab, Slack's AddEmojiDialog with
// showEmojiPacks (client-boot 8DMj, build 35b42de8b870; the store is
// core/emoji_packs.go), laid out from the 2026 stylesheet:
//
//   - AddEmojiPackDialogContent asks for the packs when it mounts
//     (fetchAllEmojiCollections, reason "emojiPack:dialog", once a
//     session) and renders nothing while the store holds none: no
//     spinner, no error (a failed read leaves the store empty and is
//     tried again on the next open).
//   - "Added" and a pill with the count when any is installed
//     (.p-emoji_pack__header: 13px bold at .7, margin 8px 0 12px; the
//     pill .04 behind, radius 10, padding 0 8, 4 from the word).
//   - the packs newest first (sortBy -date_create), a "Stay tuned" card
//     after them when there are fewer than two, a spacer to even the last
//     row, two to a row (.p-emoji_pack__pack_row: space-between, 16
//     under): each card flex 0 50% with 8px margins between, so 224 wide
//     in the 464px body, 200 tall, a 1px .13 border, radius 8, padding
//     16 16 0, clipping what doesn't fit; hovered, a 0 1px 3px #00000014
//     shadow.
//   - a card (EmojiPackCard): the name (15 bold) with a micro "New"
//     education pill when it's the newest pack and new
//     (core.EmojiPackIsNew), "by {author}" (13 at .7, one line), the
//     circle-checkbox-checked icon (#007a5a, 24px) at the right when
//     installed and not new; 16 under that, the pack's emoji four to a
//     row, 36px, space-between, 16 between rows, no tooltips.
//   - then the help line (.p-emoji_pack__help: a .13 rule above, 20 over
//     and under, 13/18 at .7), the org's name bold in its org variant,
//     20 under the last row (the row's 16 collapses into the list's 20).
//   - a click on a card opens EmojiPackDetails in the same dialog: a
//     back button (caret-left-full), the name, "by {author}" at .7, a
//     .13 "|" 8px either side, "{n} emoji"; the emoji five to a row,
//     56px, 28 between rows; installed, each a button (padding 8, radius
//     8, hovered in one of six colours by its index) that copies ":name:"
//     and closes the dialog ("Emoji copied to clipboard"), with the tip
//     "Click an emoji to copy to clipboard" in the footer, and the local
//     names. The footer's Add Pack (not installed) or Remove Pack
//     (installed, for a workspace admin, danger) loads while its call
//     runs. Added: the dialog closes and the toast "The {name} emoji pack
//     has been added to your workspace." shows; when it has gone the
//     composer's emoji button gets its badge. An add that fails leaves
//     Add Pack loading, as Slack's (its promise has no catch). Removed:
//     closes, "…has been removed from your workspace."; a failed removal
//     stops loading.
//
// .p-add_emoji_dialog caps the dialog at 700px; its body scrolls.

// nsEducation is EducationTag's (22LQ) "New".
var nsEducation = i18n.NS("education")

// Slack's InstallToast and EmojiButton toasts (Toast's fadeDelay, 6s,
// plus its 200ms close before onCloseComplete).
const (
	packToastFor  = 6 * time.Second
	packToastGone = 200 * time.Millisecond
)

// packCard is one card's click target, kept per pack id (its address is
// what a press lands on).
type packCard struct {
	click widget.Clickable
	r     image.Rectangle // body coordinates, as last drawn
}

// packEmojiBtn is one details emoji's button and tooltip.
type packEmojiBtn struct {
	click widget.Clickable
	tip   TipState
	r     image.Rectangle // body coordinates
}

// packDetails is EmojiPackDetails' state: the pack as it was clicked and
// whether it was installed then (AddEmojiDialog's f).
type packDetails struct {
	pack      core.EmojiPack
	installed bool
	emoji     core.PackEmojiList // its local emoji when installed, else its own
	btns      []*packEmojiBtn
	adding    bool
	removing  bool
}

// packsState is the tab's part of emojiAddModal.
type packsState struct {
	cards  map[string]*packCard
	detail *packDetails
	back   widget.Clickable
	backR  image.Rectangle // card coordinates
	add    *ModalButton
	remove *ModalButton
	hidden *ModalButton // the footer for its tip alone (installed, not an admin)
	custom []*ModalButton
	// helpR is the help line as last drawn (body coordinates)
	helpR image.Rectangle
	// soonR is the "Stay tuned" card as last drawn, empty when none
	soonR image.Rectangle
	// mounted: the list is showing (AddEmojiPackDialogContent mounted),
	// since when its mount effect has asked for the packs
	mounted bool
	// view is what the list draws, read from the store once per
	// EmojiPacksGen (packsView)
	view packsView
}

// packsView is the store as the list draws it: the packs newest first,
// how many are installed, the pack the "new" pill is on.
type packsView struct {
	gen       uint64
	ok        bool
	packs     []core.EmojiPack
	installed int
	newID     string
}

// packsView reads the store again when it changed (the layout runs twice a
// frame, and its cards would each read the prefs).
func (s *emojiAddModal) packsView(a *core.App) packsView {
	gen := a.EmojiPacksGen()
	if s.packs.view.ok && s.packs.view.gen == gen {
		return s.packs.view
	}
	packs, _ := a.EmojiPacks()
	sort.SliceStable(packs, func(i, j int) bool { return packs[i].DateCreate > packs[j].DateCreate })
	v := packsView{gen: gen, ok: true, packs: packs, newID: a.EmojiPackNew()}
	for _, p := range packs {
		if p.Installed {
			v.installed++
		}
	}
	s.packs.view = v
	return v
}

func (s *emojiAddModal) initPacks() {
	p := &s.packs
	p.cards = map[string]*packCard{}
	p.custom = s.modal.o.Buttons
	p.add = &ModalButton{Label: nsEmojiList.T("Add Pack"), Kind: BtnPrimary,
		Loading: func() bool { return p.detail != nil && p.detail.adding },
		Do:      func() bool { s.addPack(); return false }}
	p.remove = &ModalButton{Label: nsEmojiList.T("Remove Pack"), Kind: BtnDanger,
		Loading: func() bool { return p.detail != nil && p.detail.removing },
		Do:      func() bool { s.removePack(); return false }}
	p.hidden = &ModalButton{Hidden: func() bool { return true }}
}

// prepare is the modal's Prepare: the tab and pack clicks, then the
// header, buttons and size of the view they leave.
func (s *emojiAddModal) prepare(gtx layout.Context) {
	for i := range s.tabBtn {
		if s.tabBtn[i].Clicked(gtx) {
			s.tab = i
		}
	}
	p := &s.packs
	if p.detail != nil && p.back.Clicked(gtx) {
		p.detail = nil
	}
	if p.detail == nil && s.tab == 1 {
		for _, c := range p.cards {
			if c.click.Clicked(gtx) {
				s.openPack(c)
				break
			}
		}
	}
	if d := p.detail; d != nil {
		for i, b := range d.btns {
			if b.click.Clicked(gtx) && d.installed {
				s.copyPackEmoji(gtx, ":"+d.emoji[i].Name+":")
				return
			}
		}
	}
	o := &s.modal.o
	if p.detail != nil || s.tab != 1 {
		p.mounted = false // the list unmounts: the next showing asks again
	}
	switch {
	case p.detail != nil:
		d := p.detail
		o.Title, o.Header, o.HeadH, o.HeaderBelow, o.HeaderBelowPad = "", s.detailHeader, 82, nil, 0
		o.FooterLeft = nil
		if d.installed {
			o.FooterLeft = s.detailTip
		}
		switch {
		case !d.installed:
			o.Buttons, o.AutoFocus = []*ModalButton{p.add}, p.add
		case s.isAdmin():
			o.Buttons, o.AutoFocus = []*ModalButton{p.remove}, p.remove
		default:
			o.Buttons, o.AutoFocus = []*ModalButton{p.hidden}, nil
		}
	default:
		o.Title, o.Header, o.HeadH, o.FooterLeft, o.AutoFocus = nsEmojiList.T("Add emoji"), nil, 0, nil, nil
		o.HeaderBelow, o.HeaderBelowPad = s.tabsBelow, 16
		if s.tab == 1 {
			o.Buttons = nil
			if a := s.h.Core(); a != nil && !p.mounted {
				a.FetchEmojiPacks() // the content's mount effect (the store asks once a session)
			}
			p.mounted = true
		} else {
			o.Buttons = p.custom
		}
	}
}

// tabsBelow is the tab strip in the header under the title bar (Slack's
// ModalHeader holds the title and the tabs): drawn 12 up into the title
// bar's bottom padding, 20 tall to the rule, then the tabs' 16px margin.
func (s *emojiAddModal) tabsBelow(gtx layout.Context) layout.Dimensions {
	th := s.h.Theme()
	s.tabs(gtx, th, colorsOf(th), gtx.Constraints.Max.X)
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, dp(gtx, 20))}
}

func (s *emojiAddModal) isAdmin() bool {
	a := s.h.Core()
	if a == nil {
		return false
	}
	u := a.Self() // isCurrentUserWorkspaceAdmin (Bn/N h3)
	return u.IsAdmin || u.IsOwner
}

// openPack is onPackClick: the details of the pack the card stands for,
// installed as it was then; a click on the new pack's card sets
// emoji_packs_clicked_collection_cta.
func (s *emojiAddModal) openPack(c *packCard) {
	a := s.h.Core()
	if a == nil {
		return
	}
	packs, _ := a.EmojiPacks()
	for _, pk := range packs {
		if s.packs.cards[pk.ID] != c {
			continue
		}
		if a.EmojiPackIsNew(pk.ID) {
			a.ClickedEmojiPackCTA()
		}
		d := &packDetails{pack: pk, installed: pk.Installed, emoji: pk.Emoji}
		if pk.Installed {
			d.emoji = pk.LocalEmoji // getEmojiCollectionLocalEmoji
		}
		for range d.emoji {
			d.btns = append(d.btns, &packEmojiBtn{})
		}
		s.packs.detail = d
		return
	}
}

// addPack is EmojiPackDetails' Add Pack: addEmojiCollection, then the
// dialog closes and the InstallToast shows; its close badges the
// composer. A failed add keeps the button loading (Slack's .then has no
// catch).
func (s *emojiAddModal) addPack() {
	d := s.packs.detail
	a := s.h.Core()
	if d == nil || d.adding || a == nil {
		return
	}
	d.adding = true
	id, name := d.pack.ID, d.pack.Name
	run(s.h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.AddEmojiPack(ctx, id)
	}, func(_ struct{}, err error) {
		if err != nil {
			return
		}
		d.adding = false
		s.modal.Close()
		packToast(s.h, nsEmojiList.RT("The {name} emoji pack has been added to your workspace.", "name", name), a.BadgeEmojiPacks)
	})
}

// removePack is Remove Pack: removeEmojiCollection; on success the dialog
// closes with the removed toast, and either way the button stops loading.
func (s *emojiAddModal) removePack() {
	d := s.packs.detail
	a := s.h.Core()
	if d == nil || d.removing || a == nil {
		return
	}
	d.removing = true
	id, name := d.pack.ID, d.pack.Name
	run(s.h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.RemoveEmojiPack(ctx, id)
	}, func(_ struct{}, err error) {
		d.removing = false
		if err != nil {
			return
		}
		s.modal.Close()
		packToast(s.h, nsEmojiList.RT("The {name} emoji pack has been removed from your workspace.", "name", name), nil)
	})
}

// copyPackEmoji is EmojiButton's onClick: ":name:" to the clipboard, the
// dialog closed, "Emoji copied to clipboard".
func (s *emojiAddModal) copyPackEmoji(gtx layout.Context, name string) {
	copyText(gtx, name)
	s.modal.Close()
	slackToast(s.h, nsEmojiList.T("Emoji copied to clipboard"), packToastFor)
}

// GoneToaster is a host whose Slack Toast says when it has gone (Toast's
// onCloseComplete).
type GoneToaster interface {
	ToastPartsThen(parts []i18n.Part, d time.Duration, gone func())
}

// packToast shows an InstallToast: parts with the pack's name bold, gone
// run once it has closed.
func packToast(h Host, parts []i18n.Part, gone func()) {
	for i := range parts {
		if parts[i].Arg == "name" {
			parts[i].Tag = "b"
		}
	}
	if t, ok := h.(GoneToaster); ok {
		t.ToastPartsThen(parts, packToastFor, gone)
		return
	}
	if t, ok := h.(PartsToaster); ok {
		t.ToastParts(parts, "", color.NRGBA{}, packToastFor)
	} else {
		h.Toast(i18n.Plain(parts))
	}
	if gone != nil {
		time.AfterFunc(packToastFor+packToastGone, gone)
	}
}

// ---- the list ----

// packAlpha is Slack's rgba(var(--sk_foreground_*), a) over the dialog:
// the theme's ink at a.
func packAlpha(cl *Colors, a float64) color.NRGBA {
	return theme.WithAlpha(cl.Fg, uint8(a*255+.5))
}

// packItem is one slot of the list: a pack, the "Stay tuned" card, or a
// spacer.
type packItem struct {
	pack   *core.EmojiPack
	soon   bool
	spacer bool
}

// packsLayout draws the tab's body (AddEmojiPackDialogContent) at y 0.
func (s *emojiAddModal) packsLayout(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	a := s.h.Core()
	if a == nil {
		return layout.Dimensions{Size: image.Pt(w, 0)}
	}
	vw := s.packsView(a)
	packs, installed := vw.packs, vw.installed
	s.packs.soonR = image.Rectangle{}
	if len(packs) == 0 {
		return layout.Dimensions{Size: image.Pt(w, 0)} // isEmpty(collections): null
	}
	items := make([]packItem, 0, len(packs)+2)
	for i := range packs {
		items = append(items, packItem{pack: &packs[i]})
	}
	if len(packs) < 2 {
		items = append(items, packItem{soon: true})
	}
	if len(items)%2 != 0 {
		items = append(items, packItem{spacer: true})
	}
	y := 0
	muted := packAlpha(cl, .7)
	if installed > 0 {
		y += dp(gtx, 8)
		lh := dp(gtx, 18)
		label := nsEmojiList.T("Added")
		tw := textW(gtx, th, 13, theme.Bold, label)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Bold, muted, label)
			})
		})
		n := fmt.Sprint(installed)
		nw := textW(gtx, th, 13, theme.Bold, n)
		px := tw + dp(gtx, 4)
		pill := image.Rect(px, y, px+nw+2*dp(gtx, 8), y+lh)
		rrect(gtx, pill, dp(gtx, 10), packAlpha(cl, .04))
		at(gtx, image.Pt(px+dp(gtx, 8), y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(nw+1, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Bold, muted, n)
			})
		})
		y += lh + dp(gtx, 12)
	}
	gap := dp(gtx, 8)
	cw := (w - 2*gap) / 2
	ch := dp(gtx, 200)
	seen := make(map[string]bool, len(packs))
	for i := 0; i < len(items); i += 2 {
		for k := 0; k < 2; k++ {
			it := items[i+k]
			x := k * (cw + 2*gap)
			r := image.Rect(x, y, x+cw, y+ch)
			switch {
			case it.pack != nil:
				c := s.packs.cards[it.pack.ID]
				if c == nil {
					c = &packCard{}
					s.packs.cards[it.pack.ID] = c
				}
				seen[it.pack.ID] = true
				c.r = r
				s.packCard(gtx, th, cl, c, it.pack, r)
			case it.soon:
				s.packs.soonR = r
				packSoon(gtx, th, cl, r)
			}
		}
		y += ch + dp(gtx, 16)
	}
	for id := range s.packs.cards {
		if !seen[id] {
			delete(s.packs.cards, id)
		}
	}
	y += dp(gtx, 20) - dp(gtx, 16) // the last row's margin collapses into the list's
	// the help line
	fill(gtx, image.Rect(0, y, w, y+1), packAlpha(cl, .13))
	hy := y + 1 + dp(gtx, 20)
	parts := s.packHelp()
	hd := at(gtx, image.Pt(0, hy), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return richParts(gtx, th, 13, 18, muted, parts)
	})
	s.packs.helpR = image.Rect(0, hy, w, hy+hd.Size.Y)
	y = hy + hd.Size.Y + dp(gtx, 20)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// packHelp is the help line: the org variant, the org's name bold, on a
// grid (AddEmojiDialog's orgName); else the team's.
func (s *emojiAddModal) packHelp() []i18n.Part {
	org := ""
	if a := s.h.Core(); a != nil && a.EnterpriseID() != "" {
		if n, _, ok := a.Team(a.EnterpriseID(), 0); ok {
			org = n
		}
	}
	if org == "" {
		return []i18n.Part{{Text: nsEmojiList.T("Emoji packs let you add a whole set new of emoji for your team. They’ll appear in the custom tab of the emoji picker (the one with the Slack icon!).")}}
	}
	parts := nsEmojiList.RT("Emoji packs let you add a whole set new of emoji to every {orgName} workspace. They’ll appear in the custom tab of the emoji picker (the one with the Slack icon!).", "orgName", org)
	for i := range parts {
		if parts[i].Arg == "orgName" {
			parts[i].Tag = "b"
		}
	}
	return parts
}

// richParts lays out parts as one paragraph in gtx.Constraints.Max.X,
// "b" and "strong" runs bold, a word wherever its run puts it (no space
// where two runs meet without one: "{orgName}." keeps its stop).
func richParts(gtx layout.Context, th *theme.Theme, size, lh float32, c color.NRGBA, parts []i18n.Part) layout.Dimensions {
	w := gtx.Constraints.Max.X
	var words []emojiAddWord
	spaceAfter := true
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		r := []rune(p.Text)
		if n := len(words); n > 0 && !spaceAfter && !unicode.IsSpace(r[0]) {
			words[n-1].glue = true
		}
		spaceAfter = unicode.IsSpace(r[len(r)-1])
		bold := p.Tag == "b" || p.Tag == "strong"
		for _, f := range strings.Fields(p.Text) {
			cs := i18n.LineChunks(f)
			for i, ch := range cs {
				words = append(words, emojiAddWord{s: ch, bold: bold, glue: i < len(cs)-1})
			}
		}
	}
	space := textW(gtx, th, size, theme.Regular, "a a") - textW(gtx, th, size, theme.Regular, "aa")
	x, y, line := 0, 0, dp(gtx, lh)
	for _, wd := range words {
		wt := theme.Regular
		if wd.bold {
			wt = theme.Bold
		}
		ww := textW(gtx, th, size, wt, wd.s)
		if x > 0 && x+ww > w {
			x, y = 0, y+line
		}
		word := wd.s
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, line)}
			return vcenter(gtx, line, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, size, wt, c, word) })
		})
		x += ww
		if !wd.glue {
			x += space
		}
	}
	if len(words) == 0 {
		return layout.Dimensions{Size: image.Pt(w, 0)}
	}
	return layout.Dimensions{Size: image.Pt(w, y+line)}
}

// packCard draws EmojiPackCard in r.
func (s *emojiAddModal) packCard(gtx layout.Context, th *theme.Theme, cl *Colors, c *packCard, p *core.EmojiPack, r image.Rectangle) {
	if p.Emoji == nil {
		return // no emoji field: the card renders nothing
	}
	isNew := p.ID != "" && p.ID == s.packs.view.newID
	rad := dp(gtx, 8)
	if c.click.Hovered() {
		softShadow(gtx, r, rad, dp(gtx, 1), dp(gtx, 3), rgba(0x000000, .08))
	}
	rrect(gtx, r, rad, packAlpha(cl, .13))
	rrect(gtx, r.Inset(border1(gtx)), rad-border1(gtx), cl.ModalBg)
	at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
		sz := r.Size()
		gtx.Constraints = layout.Exact(sz)
		return c.click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			defer clipRRect(gtx, image.Rectangle{Max: sz}.Inset(border1(gtx)), rad-border1(gtx)).Pop()
			pad := border1(gtx) + dp(gtx, 16)
			inner := sz.X - 2*pad
			// the header row: name (and New), "by {author}"; the check
			checkW := 0
			if p.Installed && !isNew {
				checkW = dp(gtx, 24)
			}
			colW := inner - checkW
			nameCall, nd := record(loose(gtx, image.Pt(colW, 1<<20)), func(gtx layout.Context) layout.Dimensions {
				return packCardName(gtx, th, cl, p.Name, isNew, colW)
			})
			by := nsEmojiList.T("by {author}", "author", p.Author)
			bh := dp(gtx, 18)
			rowH := max(nd.Size.Y+bh, checkW)
			top := pad + (rowH-nd.Size.Y-bh)/2
			macroAt(gtx, nameCall, image.Pt(pad, top))
			at(gtx, image.Pt(pad, top+nd.Size.Y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(colW, bh)}
				return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, packAlpha(cl, .7), by)
				})
			})
			if checkW > 0 {
				at(gtx, image.Pt(pad+colW, pad+(rowH-checkW)/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "circle-checkbox-checked", 24, rgb(0x007a5a))
				})
			}
			// the emoji, four to a row
			es := dp(gtx, 36)
			gapX := (inner - 4*es) / 3
			y := pad + rowH + dp(gtx, 16)
			for i := 0; i < len(p.Emoji) && y < sz.Y; i += 4 {
				for k := 0; k < 4 && i+k < len(p.Emoji); k++ {
					x := pad + k*(es+gapX)
					if k == 3 {
						x = pad + inner - es // space-between: the last at the right edge
					}
					packEmojiImage(gtx, s.h, p.Emoji[i+k].URL, image.Rect(x, y, x+es, y+es))
				}
				y += es + dp(gtx, 16)
			}
			pointerCursor(gtx, sz)
			return layout.Dimensions{Size: sz}
		})
	})
}

// packCardName is the card's header: the name (15/22 bold, wrapping) and,
// for the new pack, the micro New pill 4px after it.
func packCardName(gtx layout.Context, th *theme.Theme, cl *Colors, name string, isNew bool, w int) layout.Dimensions {
	if !isNew {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 15, 22, theme.Bold, cl.Fg, name, 0)
	}
	lh := dp(gtx, 22)
	pw := packPillW(gtx, th, nsEducation.T("New"), true)
	nw := min(textW(gtx, th, 15, theme.Bold, name), w-pw-dp(gtx, 4))
	at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(max(nw, 1), lh)}
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, name)
		})
	})
	at(gtx, image.Pt(nw+dp(gtx, 4), (lh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
		return packPill(gtx, th, nsEducation.T("New"), true, packEducation(th))
	})
	return layout.Dimensions{Size: image.Pt(nw+dp(gtx, 4)+pw, lh)}
}

// packPillColors is a Tag style's background and text.
type packPillColors struct{ bg, fg color.NRGBA }

// packEducation is the education Tag (EducationTag 22LQ): base.education
// behind content.education (--dt_color-base-education and
// --dt_color-content-education, light and dark).
func packEducation(th *theme.Theme) packPillColors {
	if th != nil && th.Dark {
		return packPillColors{rgb(0x040b3d), rgb(0xe4e8f4)}
	}
	return packPillColors{rgb(0xf0f3fc), rgb(0x112377)}
}

// packNeutral is the neutral Tag: surface.primary behind primary.
func packNeutral(cl *Colors) packPillColors { return packPillColors{cl.ModalBg, cl.Fg} }

// Slack's Tag (MCIr, its classes kMS0): inline-flex, padding 0 at the
// block ends and token 1 (4px) inline, radius base, bold, 24 tall (6) or
// 20 (5) when micro, at the base and micro sizes. Panda's atomic
// stylesheet isn't among the files Clack has, so the radius and sizes
// are the tokens' usual values: 4px, 13px and 11px.
func packPillW(gtx layout.Context, th *theme.Theme, label string, micro bool) int {
	size := float32(13)
	if micro {
		size = 11
	}
	return textW(gtx, th, size, theme.Bold, label) + 2*dp(gtx, 4)
}

func packPill(gtx layout.Context, th *theme.Theme, label string, micro bool, c packPillColors) layout.Dimensions {
	size, h := float32(13), dp(gtx, 24)
	if micro {
		size, h = 11, dp(gtx, 20)
	}
	w := packPillW(gtx, th, label, micro)
	rrect(gtx, image.Rect(0, 0, w, h), dp(gtx, 4), c.bg)
	at(gtx, image.Pt(dp(gtx, 4), 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, h)}
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, size, theme.Bold, c.fg, label)
		})
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// packEmojiImage draws an emoji image fitted into r.
func packEmojiImage(gtx layout.Context, h Host, url string, r image.Rectangle) {
	if url == "" || h.Images() == nil {
		return
	}
	img, ok := h.Images().Get(url, r.Dx())
	if !ok {
		return
	}
	at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
		return containImage(gtx, img, r.Size())
	})
}

// packSoon draws EmojiPackComingSoon in r: the card at .04, "Stay tuned"
// (a neutral Tag, 8 over the text) and "More emoji packs are on the way"
// (padding 0 28) centred in the column, the illustrations' two 198×30
// strips at its top and bottom (position absolute, inset 0).
func packSoon(gtx layout.Context, th *theme.Theme, cl *Colors, r image.Rectangle) {
	rad := dp(gtx, 8)
	rrect(gtx, r, rad, packAlpha(cl, .13))
	inner := r.Inset(border1(gtx))
	rrect(gtx, inner, rad-border1(gtx), cl.ModalBg)
	rrect(gtx, inner, rad-border1(gtx), packAlpha(cl, .04))
	st := clipRRect(gtx, inner, rad-border1(gtx))
	defer st.Pop()
	// the strips (illustrations' column: space-between, centred)
	sw, sh := dp(gtx, 198), dp(gtx, 30)
	sx := inner.Min.X + (inner.Dx()-sw)/2
	packSoonArt(gtx, 0, image.Pt(sx, inner.Min.Y))
	packSoonArt(gtx, 1, image.Pt(sx, inner.Max.Y-sh))
	// the text column, centred in the padding box's content (padding 16
	// 16 0)
	label := nsEmojiList.T("Stay tuned")
	msg := nsEmojiList.T("More emoji packs are on the way")
	pad := dp(gtx, 16)
	tw := inner.Dx() - 2*pad - 2*dp(gtx, 28)
	msgCall, md := record(loose(gtx, image.Pt(max(tw, 1), 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return paraCentered(gtx, th, 15, 22, cl.Fg, msg, tw)
	})
	ph := dp(gtx, 24)
	colH := ph + dp(gtx, 8) + md.Size.Y
	top := inner.Min.Y + pad + (inner.Dy()-pad-colH)/2
	pw := packPillW(gtx, th, label, false)
	at(gtx, image.Pt(inner.Min.X+(inner.Dx()-pw)/2, top), func(gtx layout.Context) layout.Dimensions {
		return packPill(gtx, th, label, false, packNeutral(cl))
	})
	macroAt(gtx, msgCall, image.Pt(inner.Min.X+pad+dp(gtx, 28), top+ph+dp(gtx, 8)))
}

// packSoonArt draws strip i of EmojiPackComingSoon's illustration at p,
// 198×30 in its viewBox: each group at opacity .8, the first group of the
// top strip clipped to its clip0 rect (0, -18, 48×48).
func packSoonArt(gtx layout.Context, i int, p image.Point) {
	sc := float32(gtx.Metric.PxPerDp)
	defer op.Offset(p).Push(gtx.Ops).Pop()
	defer clip.Rect{Max: image.Pt(dp(gtx, 198), dp(gtx, 30))}.Push(gtx.Ops).Pop()
	for _, g := range epSoonArt[i] {
		o := paint.PushOpacity(gtx.Ops, .8)
		var cs clip.Stack
		if g.clip {
			cs = clip.Rect{Min: image.Pt(0, int(-18*sc)), Max: image.Pt(int(48*sc+.5), int(30*sc+.5))}.Push(gtx.Ops)
		}
		for _, part := range g.parts {
			st := clip.Outline{Path: icons.ClipPath(gtx, part.d, sc)}.Op().Push(gtx.Ops)
			paint.ColorOp{Color: rgb(part.fill)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
		}
		if g.clip {
			cs.Pop()
		}
		o.Pop()
	}
}

// ---- the details ----

// detailHeader is EmojiPackDetails' ModalTitleBar (min-height 70,
// padding 20 28, 76 at the right for the close button): the back
// IconButton in its 40px icon box (16 to the text), the name (18/24
// black) over the subtitle (13/18): "by {author}" at .7, a .13 "|" 8px
// either side, "{n} emoji".
func (s *emojiAddModal) detailHeader(gtx layout.Context) layout.Dimensions {
	d := s.packs.detail
	sz := gtx.Constraints.Max
	if d == nil {
		return layout.Dimensions{Size: sz}
	}
	th := s.h.Theme()
	cl := colorsOf(th)
	padX := dp(gtx, 28)
	bh := dp(gtx, 40)
	by := (sz.Y - bh) / 2
	bb := dp(gtx, 36)
	s.packs.backR = image.Rect(padX, by+(bh-bb)/2, padX+bb, by+(bh-bb)/2+bb)
	at(gtx, s.packs.backR.Min, func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &s.packs.back, "caret-left-full", 36, 20, cl.Fg2, cl.BgHover, 8)
	})
	tx := padX + bb + dp(gtx, 16)
	tw := sz.X - tx - dp(gtx, 76)
	th1, th2 := dp(gtx, 24), dp(gtx, 18)
	ty := (sz.Y - th1 - th2) / 2
	at(gtx, image.Pt(tx, ty), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(max(tw, 1), th1)}
		return vcenter(gtx, th1, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Black, cl.Fg, d.pack.Name)
		})
	})
	author := nsEmojiList.T("by {author}", "author", d.pack.Author)
	count := nsEmojiList.T("{emojiCount, plural, =1 {# emoji} other {# emoji}}", "emojiCount", len(d.pack.Emoji))
	x := tx
	sy := ty + th1
	line := func(c color.NRGBA, s string) {
		at(gtx, image.Pt(x, sy), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(tx+tw-x, 1), th2)}
			return vcenter(gtx, th2, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, c, s)
			})
		})
		x += textW(gtx, th, 13, theme.Regular, s)
	}
	line(packAlpha(cl, .7), author)
	x += dp(gtx, 8)
	line(packAlpha(cl, .13), "|")
	x += dp(gtx, 8)
	line(cl.Fg, count)
	return layout.Dimensions{Size: sz}
}

// detailTip is the footer's "Click an emoji to copy to clipboard" (at
// .5) for an installed pack.
func (s *emojiAddModal) detailTip(gtx layout.Context) layout.Dimensions {
	th := s.h.Theme()
	cl := colorsOf(th)
	h := dp(gtx, 36)
	return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 15, theme.Regular, packAlpha(cl, .5), nsEmojiList.T("Click an emoji to copy to clipboard"))
	})
}

// packHoverColors are .p-emoji_pack_details__button's hovers by
// data-color-index (n % 6).
var packHoverColors = [6]color.NRGBA{
	{0x9b, 0xdf, 0x58, 0xb3}, {0x95, 0xd3, 0xfe, 0xb3}, {0xf7, 0xe9, 0x22, 0xb3},
	{0xee, 0xa6, 0xfc, 0xb3}, {0xff, 0xd5, 0x8f, 0xb3}, {0xd3, 0xd1, 0xff, 0xb3},
}

// detailsLayout draws the details' body: the emoji five to a row
// (.p-emoji_pack_details__emoji_row: space-between, 28 under, none after
// the last), 56px, installed ones as 72px buttons (padding 8), the last
// row padded with 56px spacers.
func (s *emojiAddModal) detailsLayout(gtx layout.Context, th *theme.Theme, w int) layout.Dimensions {
	d := s.packs.detail
	es := dp(gtx, 56)
	bw := es
	if d.installed {
		bw = es + 2*dp(gtx, 8)
	}
	y := 0
	n := len(d.emoji)
	for i := 0; i < n; i += 5 {
		widths := make([]int, 0, 5)
		for k := 0; k < 5; k++ {
			if i+k < n {
				widths = append(widths, bw)
			} else {
				widths = append(widths, es)
			}
		}
		total := 0
		for _, v := range widths {
			total += v
		}
		gap := (w - total) / 4
		rh := bw
		x := 0
		for k := 0; k < 5 && i+k < n; k++ {
			idx := i + k
			e := d.emoji[idx]
			b := d.btns[idx]
			r := image.Rect(x, y+(rh-bw)/2, x+bw, y+(rh-bw)/2+bw)
			if k == 4 {
				r = image.Rect(w-bw, r.Min.Y, w, r.Max.Y)
			}
			b.r = r
			s.packEmoji(gtx, th, b, e, idx, d.installed, r)
			x += widths[k] + gap
		}
		y += rh
		if i+5 < n {
			y += dp(gtx, 28)
		}
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// packEmoji is EmojiButton: the 56px emoji with its ":name:" tooltip,
// installed a button hovered in its index's colour.
func (s *emojiAddModal) packEmoji(gtx layout.Context, th *theme.Theme, b *packEmojiBtn, e core.PackEmoji, idx int, button bool, r image.Rectangle) {
	at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
		sz := r.Size()
		gtx.Constraints = layout.Exact(sz)
		dd := b.click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if button && b.click.Hovered() {
				rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 8), packHoverColors[idx%6])
			}
			es := dp(gtx, 56)
			o := (sz.X - es) / 2
			packEmojiImage(gtx, s.h, e.URL, image.Rect(o, o, o+es, o+es))
			if button {
				pointerCursor(gtx, sz)
			} else {
				defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
				pointer.CursorDefault.Add(gtx.Ops)
			}
			return layout.Dimensions{Size: sz}
		})
		Tooltip(gtx, th, &b.tip, b.click.Hovered(), sz, Above, ":"+e.Name+":", "")
		return dd
	})
}
