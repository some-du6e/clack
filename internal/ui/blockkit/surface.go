package blockkit

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ---- text views ----

// textView draws a text object (mrkdwn or plain_text) with richview,
// cached per width.
type textView struct {
	h      Host
	v      *richview.View
	muted  bool
	hand   surfHandler
	lastW  int
	lastH  int
	lastPx float32
}

func docOfText(h Host, t Text) richtext.Doc {
	c := h.Core()
	s := t.Text
	if t.Type == "mrkdwn" && !t.Verbatim && c != nil {
		return richtext.Doc{Blocks: c.Mrkdwn(s)}
	}
	// plain_text: emoji shortcodes still render (emoji: true), nothing else
	if c != nil && !strings.ContainsAny(s, "*_~`<>&") {
		return richtext.Doc{Blocks: c.Mrkdwn(s)}
	}
	return richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: s}}}}}
}

func newTextView(h Host, t Text) *textView {
	tv := &textView{h: h, hand: surfHandler{h: h}}
	tv.v = richview.New(docOfText(h, t), richview.Options{})
	return tv
}

// textView returns the cached label view of an option text.
func (w *ew) textView(t Text) *textView {
	if w.views == nil {
		w.views = map[string]*textView{}
	}
	k := t.Type + "\x00" + t.Text
	tv := w.views[k]
	if tv == nil {
		tv = newTextView(w.o.host(), t)
		w.views[k] = tv
	}
	return tv
}

func (tv *textView) style() *richview.Style { return styleFor(tv.h.Theme(), tv.h.Images()) }

// height measures the text at width tw (laying it out off-screen).
func (tv *textView) height(gtx C, tw int) int {
	if tw == tv.lastW && gtx.Metric.PxPerDp == tv.lastPx && tv.lastH > 0 {
		return tv.lastH
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(tw, 1), 1<<20)}
	d := tv.v.Layout(g, tv.style(), &tv.hand)
	m.Stop()
	tv.lastW, tv.lastH, tv.lastPx = tw, d.Size.Y, gtx.Metric.PxPerDp
	return d.Size.Y
}

// layout draws the text at width tw; col decides normal vs muted.
func (tv *textView) layout(gtx C, tw int, col color.NRGBA) int {
	_ = col
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(tw, 1), 1<<20)}
	d := tv.v.Layout(g, tv.style(), &tv.hand)
	tv.lastW, tv.lastH, tv.lastPx = tw, d.Size.Y, gtx.Metric.PxPerDp
	return d.Size.Y
}

// mutedView makes descriptions use the secondary text color.
func (tv *textView) setMuted(m bool) {
	if tv.muted != m {
		tv.muted = m
		tv.v.SetOptions(richview.Options{Muted: m})
	}
}

// surfHandler routes clicks in surface text.
type surfHandler struct{ h Host }

func (s *surfHandler) OpenURL(u string) {
	if strings.HasPrefix(u, "slack://") {
		return
	}
	openURL(u)
}
func (s *surfHandler) OpenUser(id string, anchor image.Point) {
	s.h.OpenProfile(pointerAt(s.h, anchor), id)
}
func (s *surfHandler) OpenChannel(id string) { s.h.Navigate("c/" + id) }
func (s *surfHandler) OpenUsergroup(id string, anchor image.Point) {
}
func (s *surfHandler) OpenImage(img richtext.Image) { s.h.OpenLightbox(img, core.Message{}) }
func (s *surfHandler) OpenFile(f *richtext.File) {
	if f.Permalink != "" {
		openURL(f.Permalink)
	}
}
func (s *surfHandler) Button(b richtext.Button) {
	if b.URL != "" {
		openURL(b.URL)
	}
}

// ---- Surface ----

// Surface is a Block Kit surface: a modal's body or an App Home tab.
type Surface struct {
	h      Host
	view   *core.View
	blocks []Block
	vals   Values
	errs   map[string]string // block_id → error (response_action errors / validation)
	ws     map[string]*ew
	texts  map[string]*richview.View
	hand   surfHandler
	isHome bool
	busy   map[string]bool
	gen    uint32
	blkErr map[string]string // block_id → action error under actions blocks
	flying map[string]int    // block_id → actions in flight (elements disabled)
	// onAction lets the modal know an action went out (Slack clears a
	// failed submit's banner on any action).
	onAction func()
	// initial is the state when the view was shown (speedbump on close).
	initial string
	// fixed is the action base of a surface that isn't a Slack view (a
	// code channel's block_kit view): its container and app ids.
	fixed *core.BlockAction
}

// dirty reports whether the user changed any input since the view was
// shown (Slack compares initialViewState.values with the block state).
func (s *Surface) dirty() bool {
	for i := range s.blocks {
		for _, e := range elementsOf(&s.blocks[i]) {
			if stateful(e.Type) {
				return string(s.vals.JSON(false)) != s.initial
			}
		}
	}
	return false
}

func newSurface(h Host, v *core.View) *Surface {
	s := &Surface{h: h, hand: surfHandler{h: h}}
	s.set(v, false)
	return s
}

// set shows view v; with keep, values of elements still present stay
// (views.update keeps what the user typed, like Slack, unless the app
// sends a new initial value for a block id it changed).
func (s *Surface) set(v *core.View, keep bool) {
	old := s.vals
	s.view = v
	s.isHome = v != nil && v.Type == "home"
	s.blocks = ParseBlocks(v.Blocks)
	s.vals = Values{}
	if keep && old != nil {
		for i := range s.blocks {
			b := &s.blocks[i]
			for _, e := range elementsOf(b) {
				if pv := old.get(b.BlockID, e.ActionID); pv != nil && pv.Type == e.Type {
					s.vals.set(b.BlockID, e.ActionID, pv)
				}
			}
		}
	}
	initValues(s.vals, s.blocks)
	if len(v.State) > 0 && !keep {
		s.applyState(v.State)
	}
	s.initial = string(s.vals.JSON(false))
	s.ws = map[string]*ew{}
	s.texts = map[string]*richview.View{}
	s.errs = map[string]string{}
	s.blkErr = map[string]string{}
	s.flying = map[string]int{}
	s.gen++
}

// applyState loads view.state.values (what Slack already has).
func (s *Surface) applyState(raw json.RawMessage) {
	var st struct {
		Values map[string]map[string]json.RawMessage `json:"values"`
	}
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	for bid, acts := range st.Values {
		for aid, rv := range acts {
			v := s.vals.get(bid, aid)
			if v == nil {
				continue
			}
			var x struct {
				SelectedOption  *Option  `json:"selected_option"`
				SelectedOptions []Option `json:"selected_options"`
				SelectedUser    string   `json:"selected_user"`
				SelectedUsers   []string `json:"selected_users"`
				SelectedConv    string   `json:"selected_conversation"`
				SelectedConvs   []string `json:"selected_conversations"`
				SelectedChan    string   `json:"selected_channel"`
				SelectedChans   []string `json:"selected_channels"`
				SelectedDate    string   `json:"selected_date"`
				SelectedTime    string   `json:"selected_time"`
				SelectedDT      int64    `json:"selected_date_time"`
				Value           *string  `json:"value"`
			}
			if json.Unmarshal(rv, &x) != nil {
				continue
			}
			switch {
			case x.SelectedOption != nil:
				v.SelectedOption = x.SelectedOption
			case x.SelectedOptions != nil:
				v.SelectedOptions = x.SelectedOptions
			case x.SelectedUser != "":
				v.SelectedUser = x.SelectedUser
			case x.SelectedUsers != nil:
				v.SelectedUsers = x.SelectedUsers
			case x.SelectedConv != "":
				v.SelectedConv = x.SelectedConv
			case x.SelectedConvs != nil:
				v.SelectedConvs = x.SelectedConvs
			case x.SelectedChan != "":
				v.SelectedChan = x.SelectedChan
			case x.SelectedChans != nil:
				v.SelectedChans = x.SelectedChans
			case x.SelectedDate != "":
				v.SelectedDate = x.SelectedDate
			case x.SelectedTime != "":
				v.SelectedTime = x.SelectedTime
			case x.SelectedDT != 0:
				v.SelectedDateTime = x.SelectedDT
			case x.Value != nil:
				v.Text = *x.Value
			}
		}
	}
}

// owner implementation
func (s *Surface) host() Host     { return s.h }
func (s *Surface) values() Values { return s.vals }
func (s *Surface) channel() string {
	if s.view != nil {
		return s.view.ChannelID
	}
	return ""
}
func (s *Surface) modal() bool { return !s.isHome }
func (s *Surface) changed() {
	s.gen++
	if s.h != nil {
		s.h.Invalidate()
	}
}
func (s *Surface) base() core.BlockAction {
	if s.fixed != nil {
		return *s.fixed
	}
	return s.h.Core().Interactive().ViewContainer(s.view)
}

// NewCodeChannelSurface is CodeChannelBlockKitView's surface: the view's
// blocks (client.codeChannels.getBlockKitView), whose actions go out
// through sendActions with the view's service_id and app_id and Slack's
// container {type: code_channel_view, view_id, channelId}.
func NewCodeChannelSurface(h Host, blocks json.RawMessage, ch, viewID, serviceID, appID string) *Surface {
	s := newSurface(h, &core.View{ID: viewID, Type: "code_channel_view", Blocks: blocks, ChannelID: ch})
	s.fixed = &core.BlockAction{ServiceID: serviceID, AppID: appID,
		Container: core.ActionContainer{Type: "code_channel_view", ChannelID: ch, ViewID: viewID}}
	return s
}

func (s *Surface) clearErr(bid string) {
	if s.errs[bid] != "" {
		delete(s.errs, bid)
		s.changed()
	}
}

func (s *Surface) fire(w *ew, action json.RawMessage, done func(error)) {
	if s.onAction != nil {
		s.onAction()
	}
	b := s.base()
	b.Action = action
	b.State = s.vals.JSON(false)
	bid := w.bid
	delete(s.blkErr, bid)
	s.flying[bid]++
	sendAction(s.h, b, func(err error) {
		if s.flying[bid]--; s.flying[bid] <= 0 {
			delete(s.flying, bid)
		}
		if err != nil {
			s.blkErr[bid] = errMessage(err)
		}
		done(err)
	})
}

// loading reports an action of block bid in flight.
func (s *Surface) loading(bid string) bool { return s.flying[bid] > 0 }

func (s *Surface) widget(b *Block, e *Element) *ew {
	k := b.BlockID + "\x00" + e.ActionID + "\x00" + e.Type
	w := s.ws[k]
	if w == nil {
		w = newEW(s, b, e, b.Type == "input")
		s.ws[k] = w
	}
	return w
}

// validate checks required inputs and formats before a submit, the way
// Slack's client does; it reports whether the form may go.
func (s *Surface) validate() bool {
	ok := true
	for i := range s.blocks {
		b := &s.blocks[i]
		if b.Type != "input" || b.Element == nil {
			continue
		}
		e := b.Element
		v := s.vals.get(b.BlockID, e.ActionID)
		msg := ""
		switch {
		case !b.Optional && v.empty() && stateful(e.Type):
			msg = "Please complete this required field."
		case v != nil && !v.empty() && e.MinLength > 0 && len([]rune(v.Text)) < e.MinLength && isTextInput(e.Type) && e.Type != "rich_text_input":
			msg = "Please enter at least " + strconv.Itoa(e.MinLength) + " characters."
		case v != nil && v.Text != "" && e.Type == "email_text_input":
			if _, err := mail.ParseAddress(v.Text); err != nil {
				msg = "Please enter a valid email address."
			}
		case v != nil && v.Text != "" && e.Type == "url_text_input":
			if u, err := url.Parse(v.Text); err != nil || u.Scheme == "" || u.Host == "" {
				msg = "Please enter a valid URL."
			}
		case v != nil && v.Text != "" && e.Type == "number_input":
			f, err := strconv.ParseFloat(strings.ReplaceAll(v.Text, ",", "."), 64)
			switch {
			case err != nil || (!e.IsDecimalAllowed && strings.ContainsAny(v.Text, ".,")):
				msg = "Please enter a valid number."
			case e.MinValue != "" && f < atof(e.MinValue):
				msg = "Please enter a number greater than or equal to " + e.MinValue + "."
			case e.MaxValue != "" && f > atof(e.MaxValue):
				msg = "Please enter a number less than or equal to " + e.MaxValue + "."
			}
		}
		if msg != "" {
			s.errs[b.BlockID] = msg
			ok = false
		}
	}
	if !ok {
		s.changed()
	}
	return ok
}

func atof(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }

// textOfBlock is a non-interactive block as a richtext doc.
func (s *Surface) docOf(b *Block) richtext.Doc {
	c := s.h.Core()
	if c == nil {
		return richtext.Doc{}
	}
	raw, _ := json.Marshal([]json.RawMessage{b.Raw})
	return richtext.FromMessage(c.S, &slack.Message{Blocks: raw})
}

func (s *Surface) textOf(key string, doc func() richtext.Doc) *richview.View {
	v := s.texts[key]
	if v == nil {
		v = richview.New(doc(), richview.Options{})
		s.texts[key] = v
	}
	return v
}

// Layout draws the surface at the constraint width; returns its height.
func (s *Surface) Layout(gtx C) D {
	drain()
	w := gtx.Constraints.Max.X
	y := 0
	st := styleFor(s.h.Theme(), s.h.Images())
	for i := range s.blocks {
		b := &s.blocks[i]
		gap := dp(gtx, 8)
		if i == 0 {
			gap = dp(gtx, 4)
		}
		y += gap
		var bh int
		at(gtx, image.Pt(0, y), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			bh = s.block(gtx, b, i, w, st)
			return D{}
		})
		y += bh
	}
	if len(s.blocks) > 0 {
		y += dp(gtx, 4)
	}
	return D{Size: image.Pt(w, y)}
}

func (s *Surface) block(gtx C, b *Block, i int, w int, st *richview.Style) int {
	th := s.h.Theme()
	cl := colorsOf(th)
	key := strconv.Itoa(i) + "\x00" + b.BlockID
	switch b.Type {
	case "input":
		return s.inputBlock(gtx, b, key, w)
	case "actions":
		h := s.flow(gtx, b, w)
		if msg := s.blkErr[b.BlockID]; msg != "" {
			at(gtx, image.Pt(0, h+dp(gtx, 4)), func(gtx C) D { return errorLine(gtx, th, cl, w, msg) })
			h += dp(gtx, 24)
		}
		return h
	case "section":
		return s.section(gtx, b, key, w, st)
	}
	v := s.textOf(key, func() richtext.Doc { return s.docOf(b) })
	d := v.Layout(gtx, st, &s.hand)
	return d.Size.Y
}

func errorLine(gtx C, th *theme.Theme, cl colors, w int, msg string) D {
	at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx C) D { return icon(gtx, "warning", 16, cl.ErrorText) })
	at(gtx, image.Pt(dp(gtx, 20), 0), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 20), dp(gtx, 20))}
		return label(gtx, th, 13, theme.Regular, cl.ErrorText, msg)
	})
	return D{Size: image.Pt(w, dp(gtx, 20))}
}

// flow lays out an actions block's elements, wrapping, 8px apart. In
// modals Slack makes it a grid: two equal columns, every element (28px
// tall) filling its column, rows 8px apart.
func (s *Surface) flow(gtx C, b *Block, w int) int {
	x, y, rowH := 0, 0, 0
	gap := dp(gtx, 8)
	if s.modal() {
		colW := (w - gap) / 2
		n := 0
		for i := range b.Elements {
			e := &b.Elements[i]
			ew := s.widget(b, e)
			sz := ew.size(gtx, colW, false)
			if sz.X == 0 {
				continue
			}
			if !stacks(e) && !isTextInput(e.Type) && e.Type != "datetimepicker" {
				sz.X = colW
			} else {
				sz.X = min(sz.X, colW)
			}
			cx := (n % 2) * (colW + gap)
			if n%2 == 0 && n > 0 {
				y += rowH + gap
				rowH = 0
			}
			at(gtx, image.Pt(cx, y), func(gtx C) D { return ew.layout(fixed(gtx, sz), sz) })
			rowH = max(rowH, sz.Y)
			n++
		}
		return y + rowH
	}
	for i := range b.Elements {
		e := &b.Elements[i]
		ew := s.widget(b, e)
		sz := ew.size(gtx, w, false)
		if sz.X == 0 {
			continue
		}
		if x > 0 && x+sz.X > w {
			x = 0
			y += rowH + gap
			rowH = 0
		}
		at(gtx, image.Pt(x, y), func(gtx C) D { return ew.layout(fixed(gtx, sz), sz) })
		x += sz.X + gap
		rowH = max(rowH, sz.Y)
	}
	return y + rowH
}

func (s *Surface) section(gtx C, b *Block, key string, w int, st *richview.Style) int {
	acc := b.Accessory
	var accW, accH int
	var aw *ew
	stack := false
	imgAcc := acc != nil && acc.Type == "image"
	if acc != nil && !imgAcc {
		aw = s.widget(b, acc)
		stack = stacks(acc)
		if !stack {
			sz := aw.size(gtx, w/2, true)
			accW, accH = sz.X, sz.Y
		}
	}
	if imgAcc {
		accW, accH = dp(gtx, 88), dp(gtx, 88)
	}
	tw := w
	if accW > 0 {
		tw = w - accW - dp(gtx, 12)
	}
	y := 0
	if b.Text != nil {
		v := s.textOf(key+"\x00t", func() richtext.Doc { return docOfText(s.h, *b.Text) })
		d := at(gtx, image.Point{}, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, 1<<20)}
			return v.Layout(gtx, st, &s.hand)
		})
		y += d.Size.Y
	}
	if len(b.Fields) > 0 {
		if y > 0 {
			y += dp(gtx, 8)
		}
		colW := (tw - dp(gtx, 16)) / 2
		rowY, rowH := y, 0
		for fi := range b.Fields {
			f := b.Fields[fi]
			v := s.textOf(key+"\x00f"+strconv.Itoa(fi), func() richtext.Doc { return docOfText(s.h, f) })
			x := 0
			if fi%2 == 1 {
				x = colW + dp(gtx, 16)
			}
			d := at(gtx, image.Pt(x, rowY), func(gtx C) D {
				gtx.Constraints = layout.Constraints{Max: image.Pt(colW, 1<<20)}
				return v.Layout(gtx, st, &s.hand)
			})
			rowH = max(rowH, d.Size.Y)
			if fi%2 == 1 || fi == len(b.Fields)-1 {
				rowY += rowH + dp(gtx, 8)
				rowH = 0
			}
		}
		y = rowY - dp(gtx, 8)
	}
	switch {
	case imgAcc:
		u := acc.Raw
		var im struct {
			URL string `json:"image_url"`
			Alt string `json:"alt_text"`
		}
		_ = json.Unmarshal(u, &im)
		if img, ok := s.h.Images().Get(richtext.SafeImage(im.URL), accW); ok {
			at(gtx, image.Pt(w-accW, 0), func(gtx C) D { return theme.Image(gtx, img, image.Pt(accW, accH), dp(gtx, 8)) })
		} else {
			at(gtx, image.Pt(w-accW, 0), func(gtx C) D { return s.h.Theme().Placeholder(gtx, image.Pt(accW, accH), dp(gtx, 8)) })
		}
		y = max(y, accH)
	case aw != nil && stack:
		if y > 0 {
			y += dp(gtx, 12)
		}
		sz := aw.size(gtx, w, true)
		at(gtx, image.Pt(0, y), func(gtx C) D { return aw.layout(fixed(gtx, sz), sz) })
		y += sz.Y
	case aw != nil:
		sz := image.Pt(accW, accH)
		at(gtx, image.Pt(w-accW, 0), func(gtx C) D { return aw.layout(fixed(gtx, sz), sz) })
		y = max(y, accH)
	}
	if msg := s.blkErr[b.BlockID]; msg != "" {
		at(gtx, image.Pt(0, y+dp(gtx, 4)), func(gtx C) D { return errorLine(gtx, s.h.Theme(), colorsOf(s.h.Theme()), w, msg) })
		y += dp(gtx, 24)
	}
	return y
}

// inputBlock: label row (30), control, hint, error.
func (s *Surface) inputBlock(gtx C, b *Block, key string, w int) int {
	th := s.h.Theme()
	cl := colorsOf(th)
	y := 0
	lbl := textOf(b.Label)
	at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx C) D {
		d := label(gtx, th, 15, theme.Bold, cl.Text, lbl)
		if b.Optional {
			at(gtx, image.Pt(d.Size.X+dp(gtx, 4), 0), func(gtx C) D {
				return label(gtx, th, 15, theme.Regular, cl.Hint, "(optional)")
			})
		}
		return d
	})
	y += dp(gtx, 30)
	if b.Element != nil {
		ew := s.widget(b, b.Element)
		sz := ew.size(gtx, w, false)
		at(gtx, image.Pt(0, y), func(gtx C) D { return ew.layout(fixed(gtx, sz), sz) })
		y += sz.Y
	}
	if msg := s.errs[b.BlockID]; msg != "" {
		at(gtx, image.Pt(0, y+dp(gtx, 4)), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, cl.ErrorText, msg)
		})
		y += dp(gtx, 4) + dp(gtx, 18)
	}
	if b.Hint != nil {
		v := s.textOf(key+"\x00h", func() richtext.Doc { return docOfText(s.h, *b.Hint) })
		v.SetOptions(richview.Options{Muted: true, Compact: true})
		d := at(gtx, image.Pt(0, y+dp(gtx, 4)), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return v.Layout(gtx, styleFor(th, s.h.Images()), &s.hand)
		})
		y += dp(gtx, 4) + d.Size.Y
	}
	if s.errs[b.BlockID] == "" && enterHint(b) {
		// InputBlockContent's p-block_kit_input_block__enter_help_text
		at(gtx, image.Pt(0, y+dp(gtx, 4)), func(gtx C) D {
			x := 0
			if icons.Has("enter") {
				is := dp(gtx, 13)
				at(gtx, image.Pt(0, (dp(gtx, 18)-is)/2), func(gtx C) D { return icon(gtx, "enter", 13, cl.Hint) })
				x = is + dp(gtx, 4)
			} else {
				x = label(gtx, th, 13, theme.Regular, cl.Hint, "⏎").Size.X + dp(gtx, 4)
			}
			at(gtx, image.Pt(x, 0), func(gtx C) D {
				return label(gtx, th, 13, theme.Regular, cl.Hint, "Press 'enter' to submit")
			})
			return D{}
		})
		y += dp(gtx, 4) + dp(gtx, 18)
	}
	return y
}

// submitValues is the state to send with views.submit.
func (s *Surface) submitValues() json.RawMessage { return s.vals.JSON(false) }

// errMessage is the text Slack shows for a failed action.
func errMessage(err error) string {
	if ae, ok := err.(*core.ActionError); ok {
		return ae.Error()
	}
	if err == context.DeadlineExceeded {
		return core.ActionErrorText("post_error")
	}
	return core.ActionErrorText("")
}

// sendAction dispatches blocks.actions (through Taut's sendActions when
// a plugin patches it) and calls done on the UI goroutine.
func sendAction(h Host, b core.BlockAction, done func(error)) {
	run(h, 15*time.Second, func(ctx context.Context) (string, error) {
		return dispatchSendActions(ctx, h, b)
	}, func(_ string, err error) {
		if done != nil {
			done(err)
		}
	})
}
