package blockkit

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
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
	lastFG uint32 // theme.FontGen of lastH
}

// docOfText is a text object as a doc. A mrkdwn one is formatted whatever
// its verbatim: Slack's MrkdwnElement (boot-shared WoLU) hands the text to
// Mrkdwn and TSF as it is, ``` blocks and all; verbatim only tells Slack's
// server not to link the names and URLs in it.
func docOfText(h Host, t Text) richtext.Doc {
	c := h.Core()
	s := t.Text
	if t.Type == "mrkdwn" && c != nil {
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
	if tw == tv.lastW && gtx.Metric.PxPerDp == tv.lastPx && tv.lastH > 0 && tv.lastFG == theme.FontGen() {
		return tv.lastH
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(tw, 1), 1<<20)}
	d := tv.v.Layout(g, tv.style(), &tv.hand)
	m.Stop()
	tv.lastW, tv.lastH, tv.lastPx, tv.lastFG = tw, d.Size.Y, gtx.Metric.PxPerDp, theme.FontGen()
	return d.Size.Y
}

// layout draws the text at width tw; col decides normal vs muted.
func (tv *textView) layout(gtx C, tw int, col color.NRGBA) int {
	_ = col
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(tw, 1), 1<<20)}
	d := tv.v.Layout(g, tv.style(), &tv.hand)
	tv.lastW, tv.lastH, tv.lastPx, tv.lastFG = tw, d.Size.Y, gtx.Metric.PxPerDp, theme.FontGen()
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

func (s *surfHandler) OpenURL(u string) { s.follow(u, false) }

// OpenMrkdwnLink is a mrkdwn link's click (surface text renders through
// Slack's BaseMrkdwnLink: openThreadForRootMessage).
func (s *surfHandler) OpenMrkdwnLink(u, _ string) { s.follow(u, true) }

// OpenMessageMention is a message_mention pill's click: MessageMention
// (p9lB) is the same component in a surface as in a message,
// jumpToMessage with the pill's own channel_id, message_ts and thread_ts.
// It never opens the element's url.
func (s *surfHandler) OpenMessageMention(ch, ts, thread, _ string) {
	if mj, ok := s.h.(interface{ JumpToMessage(ch, ts, thread string) }); ok {
		mj.JumpToMessage(ch, ts, thread)
	}
}

// follow is Slack's Link click (the ui root's FollowLink: a slack: link
// to handleDeepLink, a permalink through openPermalink), else the
// browser; without the ui root a slack: link does nothing.
func (s *surfHandler) follow(u string, openRoot bool) {
	if lf, ok := s.h.(interface{ FollowLink(string, bool) bool }); ok {
		if !lf.FollowLink(u, openRoot) && !strings.HasPrefix(u, "slack://") {
			openURL(u)
		}
		return
	}
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

// OpenFile is handleFileContainerClick for a file in a surface
// (popups.OpenFileCard): downloads in the app, not the browser.
func (s *surfHandler) OpenFile(f *richtext.File) { popups.OpenFileCard(s.h, f, openURL) }

// ChartAction is a data_visualization block's More actions: its menu.
func (s *surfHandler) ChartAction(dv *richtext.DataViz, btn image.Rectangle, window bool) {
	if mn := popups.NewChartMenu(s.h, popups.ActionAnchor(btn, window), dv); mn != nil {
		s.h.ShowOverlay(mn)
	}
}

// ContactCardAction is a contact card's action.
func (s *surfHandler) ContactCardAction(act string, c *richtext.ContactCard, _ image.Rectangle, _ bool) {
	popups.ContactCardAction(s.h, act, c)
}

// SharePromptSend is a share_prompt's Send to {agentName}.
func (s *surfHandler) SharePromptSend(p *richtext.SharePrompt) { popups.SharePromptSend(s.h, p) }

// EventAction is an event card's action.
func (s *surfHandler) EventAction(act string, ev *richtext.Event, ch, ts string, btn image.Rectangle, window bool) {
	popups.EventAction(s.h, act, ev, ch, ts, popups.ActionAnchor(btn, window))
}

// DownloadImageFile is an image's AttachmentImageActions Download: the
// image's Slack file.
func (s *surfHandler) DownloadImageFile(f *richtext.File) {
	if f != nil {
		popups.DownloadFile(s.h, f)
	}
}

// ImageMenu is an image's More actions (AttachmentImageActions' menu, which
// Slack's app views keep): Copy link, and Hide image when it has an
// expandable.
func (s *surfHandler) ImageMenu(href, key string, btn image.Rectangle, closed func()) {
	s.h.ShowOverlay(popups.NewImageActionsMenu(s.h, popups.ActionAnchor(btn, false), href, key, closed))
}

// TimelineSources is a timeline section's "(Sources)": its sources modal.
func (s *surfHandler) TimelineSources(sec *richtext.TimelineSection, _ image.Rectangle) {
	popups.OpenTimelineSources(s.h, sec)
}

// DataTableAction is a data_table's header button or column header.
func (s *surfHandler) DataTableAction(act string, c *richview.TableControl, btn image.Rectangle) {
	popups.DataTableAction(s.h, act, c, popups.ActionAnchor(btn, c.Window))
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
	// prevTexts are the last view's block views, which a views.update
	// takes over by key (textOf): ViewBlocks' SUIRenderer keeps each item
	// by its React key, so ContainerBlock's collapse state survives
	prevTexts map[string]*richview.View
	hand      surfHandler
	isHome    bool
	// asMessage: the view's blocks draw as a message's do
	// (InteractiveBlocks with getMessageOverrides, boot-shared VoyB,
	// nllw), a code channel view's
	asMessage bool
	busy      map[string]bool
	gen       uint32
	blkErr    map[string]string // block_id → action error under actions blocks
	flying    map[string]int    // block_id → actions in flight (elements disabled)
	// onAction lets the modal know an action went out (Slack clears a
	// failed submit's banner on any action).
	onAction func()
	// initial is the state when the view was shown (speedbump on close).
	initial string
	// fixed is the action base of a surface that isn't a Slack view (a
	// code channel's block_kit view): its container and app ids.
	fixed *core.BlockAction
	// org is the window position of the surface's top-left as the modal
	// last drew it; curY the y of the block being laid out (ModalFields).
	org  image.Point
	curY int
	// nested hosts the elements of blocks richview draws (Surface.block)
	nested *nestedElements
}

// dirty reports whether the user changed any input since the view was
// shown (Slack compares initialViewState.values with the block state).
func (s *Surface) dirty() bool {
	for _, b := range allBlocks(s.blocks) {
		for _, e := range elementsOf(b) {
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
	s.asMessage = v != nil && v.Type == "code_channel_view"
	s.blocks = ParseBlocks(expandConditions(s.h, v.Blocks))
	s.vals = Values{}
	if keep && old != nil {
		for _, b := range allBlocks(s.blocks) {
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
	// a views.update keeps the widgets of elements still there (same
	// block_id, action_id and type) whose values it kept: a focused input
	// keeps the focus and what was typed, as in Slack, where React
	// reconciles the input by its key
	oldWS := s.ws
	s.ws = map[string]*ew{}
	if keep {
		for _, b := range allBlocks(s.blocks) {
			for _, e := range elementsOf(b) {
				k := surfKey(b, e)
				if w := oldWS[k]; w != nil && s.vals.get(b.BlockID, e.ActionID) == old.get(b.BlockID, e.ActionID) {
					w.rebind(b, e)
					s.ws[k] = w
				}
			}
		}
	}
	s.prevTexts = nil
	if keep {
		s.prevTexts = s.texts
	}
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
func (s *Surface) modal() bool { return !s.isHome && !s.asMessage }
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
	if _, had := s.blkErr[bid]; had {
		delete(s.blkErr, bid)
		s.gen++ // a nested block's error line goes (ElementsVersion)
	}
	s.flying[bid]++
	sendAction(s.h, b, func(err error) {
		if s.flying[bid]--; s.flying[bid] <= 0 {
			delete(s.flying, bid)
		}
		if err != nil {
			s.blkErr[bid] = errMessage(err)
			s.gen++ // a nested block lays its error line out (ElementsVersion)
		}
		done(err)
	})
}

// loading reports an action of block bid in flight.
func (s *Surface) loading(bid string) bool { return s.flying[bid] > 0 }

// surfKey is an element's widget key: its block, action_id and type, and
// in a data_table its value (the rows may share one action_id, each
// cell's button its own value).
func surfKey(b *Block, e *Element) string {
	k := b.BlockID + "\x00" + e.ActionID + "\x00" + e.Type
	if b.Type == "data_table" {
		k += "\x00" + e.Value
	}
	return k
}

func (s *Surface) widget(b *Block, e *Element) *ew {
	k := surfKey(b, e)
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
	// top-level inputs only: one nested in a container or a callout draws
	// through richview (a section with its element), with no error line
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

// selfID is the viewer's member id ("" without a workspace).
func (s *Surface) selfID() string {
	if c := s.h.Core(); c != nil && c.S != nil {
		return c.S.SelfID()
	}
	return ""
}

// docOf is a non-interactive block as a richtext doc.
func (s *Surface) docOf(b *Block) richtext.Doc {
	c := s.h.Core()
	if c == nil {
		return richtext.Doc{}
	}
	raw, _ := json.Marshal([]json.RawMessage{b.Raw})
	return richtext.FromMessage(c.S, &slack.Message{Blocks: raw})
}

func (s *Surface) textOf(key string, doc func() richtext.Doc) *richview.View {
	return s.textWith(key, richview.Options{FillBlocks: true}, doc) // a view's blocks: no flex wrappers
}

// textWith is textOf with o, the options the view is made with.
func (s *Surface) textWith(key string, o richview.Options, doc func() richtext.Doc) *richview.View {
	v := s.texts[key]
	if v == nil {
		if old := s.prevTexts[key]; old != nil { // a views.update: the block's view, its toggles kept
			delete(s.prevTexts, key)
			v = old
			v.Set(doc(), o)
		} else {
			v = richview.New(doc(), o)
		}
		s.texts[key] = v
	}
	return v
}

// surfaceKey is block b's key (the i-th of the view), the way ViewBlocks'
// SUIRenderer keys its items (Q5CE: block_id, else type-index), with its
// type: React remounts an item whose component changes.
func surfaceKey(b *Block, i int) string {
	if b.BlockID != "" {
		return "id\x00" + b.BlockID + "\x00" + b.Type
	}
	return "ix\x00" + strconv.Itoa(i) + "\x00" + b.Type
}

// Layout draws the surface at the constraint width; returns its height.
//
// Slack puts each top-level block in a wrapper div of its own (ViewBlocks'
// wrapViewBlock and AppHomeBlocks' wrapAppHomeBlock, boot-shared WSwB),
// none of them flex boxes but App Home's actions wrapper, so a block's
// margins collapse with its wrapper's and its neighbours' (blockMargins,
// wrapperTop). The first block's top margin and the last one's bottom
// stay inside: the modal's dialog body has padding, App Home's
// .p-app_home__blockkit is flow-root. Blocks drawn as a message's
// (asMessage) are in flex wrappers, where margins add up.
func (s *Surface) Layout(gtx C) D {
	drain()
	w := gtx.Constraints.Max.X
	y := 0
	st := styleFor(s.h.Theme(), s.h.Images())
	prevBottom, afterContext := 0, false // the last block's outer bottom margin, a context drawn
	for i := range s.blocks {
		b := &s.blocks[i]
		top, bottom, inner, innerBottom := s.blockMargins(b, i == 0)
		gap := top
		switch {
		case i == 0:
		case s.asMessage:
			gap = prevBottom + top // flex wrappers: nothing collapses
		default:
			gap = max(prevBottom, s.wrapperTop(b, afterContext), top)
		}
		y += dp(gtx, float32(gap+inner))
		prevBottom = bottom
		afterContext = afterContext || b.Type == "context"
		var bh int
		s.curY = y
		at(gtx, image.Pt(0, y), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			var bm slackc.InspectMark // Slack's block wrapper, for the inspect tool
			if slackc.Inspecting() {
				bm = bkBlockClass.Inspect(blockMods(b.Type, i == 0))
			}
			bh = s.block(gtx, b, i, w, st)
			bm.End(gtx, image.Pt(w, bh))
			return D{}
		})
		y += bh + dp(gtx, float32(innerBottom))
	}
	y += dp(gtx, float32(prevBottom)) // the last block's margin, inside the body's padding
	return D{Size: image.Pt(w, y)}
}

// blockMargins are block b's margins on the surface, in dp, as Slack's CSS
// gives them. top and bottom are its outer margins, which collapse with
// its wrapper's (wrapperTop) and its neighbours'. inner and innerBottom
// are the space inside its wrapper above and below what Clack draws, which
// doesn't collapse: a block's padding, App Home's flex actions wrapper.
// home is App Home, where each block keeps its own CSS margins; else a
// modal, whose *_block_wrapper--modal_view rules take a section's, a
// divider's, an image's and a context's away (margin 0).
func (s *Surface) blockMargins(b *Block, first bool) (top, bottom, inner, innerBottom int) {
	if s.asMessage {
		return s.messageMargins(b, first)
	}
	home := s.isHome
	switch b.Type {
	case "actions":
		if home {
			// .p-actions_block_wrapper--app_home_view{display:flex}: the
			// block's margin-bottom and its actions' 8px tops stay inside
			return 0, 0, 8, 8
		}
		// .p-actions_block{margin-bottom:8px}, and every action 8px down
		// inside the flex row (.p-actions_block_wrapper--modal_view
		// .p-actions_block__action{margin:8px 0 0})
		return 0, 8, 8, 0
	case "header":
		// .p-header_block{margin:1em 0 4px} at 15px; the first block's 4px
		// is a message's (.p-block_kit_renderer>:first-child .p-header_block),
		// and a view's blocks have no p-block_kit_renderer
		return 15, 4, 0, 0
	case "rich_text":
		// .p-rich_text_block has none of its own here (.c-message's 8px 0
		// -4px is a message's): its first and last elements' collapse
		// through it
		return richEdgeMargin(b.richFirst, true), richEdgeMargin(b.richLast, false), 0, 0
	case "section":
		if home {
			return 8, 4, 0, 0 // .p-section_block{margin:8px 0 4px}
		}
	case "divider", "image":
		if home {
			return 8, 8, 0, 0 // .p-divider_block, .p-image_block{margin:8px 0}
		}
	case "context":
		if home {
			return 4, 4, 0, 0 // .p-context_block{margin:4px 0}
		}
	case "input":
		if home {
			return 12, 12, 0, 0 // .p-input_block_wrapper--app_home_view{margin:12px 0}
		}
	case "plan", "task_card":
		return 8, 8, 0, 0 // PlanBlock's and TaskCard's .block{margin:8px 0}
	}
	return kitMargins(b)
}

// kitMargins are the margins of block b's own CSS where no surface
// overrides them, in dp (as blockMargins): the newer blocks' and
// UnsupportedBlockFallback's.
func kitMargins(b *Block) (top, bottom, inner, innerBottom int) {
	switch b.Type {
	case "alert", "card", "carousel":
		return 8, 8, 0, 0 // .alertBlock, .card, .carousel: margin 8px 0
	case "container":
		return 16, 8, 0, 0 // .containerBlock{margin-top:16px;margin-bottom:8px}
	case "data_table":
		return 4, 4, 0, 0 // .dataTableBlockOuterContainer{margin-top:4px;margin-bottom:4px}
	case "video":
		return 0, 0, 8, 8 // VideoBlock's root, .p-video_block__body{padding:8px 0}
	}
	if !richtext.Renderable(b.Type) && len(b.Nested) == 0 {
		// UnsupportedBlockFallback: .fallback{margin:8px 0}; a fallback
		// payload draws its own blocks (resolveFallback), their margins
		// inside richview's view
		return 8, 8, 0, 0
	}
	return 0, 0, 0, 0
}

// messageMargins are block b's margins where the blocks draw as a
// message's (asMessage), in dp (as blockMargins). Each block sits in a
// .p-block_kit_renderer__block_wrapper flex box, so they add up. The
// first block loses a section's, an image's and a slim plan's top margin
// (p-block_kit_renderer__block_wrapper--first) and a header keeps 4px of
// its (.p-block_kit_renderer>:first-child .p-header_block). A rich_text
// is a flex item there: its elements' margins stay inside it.
func (s *Surface) messageMargins(b *Block, first bool) (top, bottom, inner, innerBottom int) {
	switch b.Type {
	case "section":
		if first {
			return 0, 4, 0, 0
		}
		return 8, 4, 0, 0 // .p-section_block{margin:8px 0 4px}
	case "header":
		if first {
			return 4, 4, 0, 0
		}
		return 15, 4, 0, 0
	case "image":
		if first {
			return 0, 8, 0, 0
		}
		return 8, 8, 0, 0
	case "divider":
		return 8, 8, 0, 0
	case "context":
		return 4, 4, 0, 0
	case "actions":
		return 0, 8, 8, 0
	case "rich_text":
		return 0, 0, richEdgeMargin(b.richFirst, true), richEdgeMargin(b.richLast, false)
	case "plan", "task_card":
		if first && s.slimTaskCards() {
			return 0, 8, 0, 0 // .p-block_kit_renderer__block_wrapper--first .blockSlim
		}
		return 8, 8, 0, 0
	}
	return kitMargins(b)
}

// slimTaskCards is agent_streaming_slimmer_task_cards, as Message reads
// it (agent_card.go).
func (s *Surface) slimTaskCards() bool {
	c := s.h.Core()
	return c == nil || c.Experiment("agent_streaming_slimmer_task_cards") == "on"
}

// ownsMargins: Layout draws the outer margins of a block of type t
// (blockMargins) and its richview doesn't (NoOuterMargins), so they
// collapse as Slack's do. The others' richview margins are space inside
// them (a table's .tableMargin padding, a call card's) or none.
func ownsMargins(t string) bool {
	switch t {
	case "rich_text", "divider", "image", "context", "header", "alert", "card", "container":
		return true
	}
	return false
}

// wrapperTop is the top margin, in dp, of block b's wrapper in a modal or
// App Home when a block comes before it. A modal's are 12px apart
// (.p-block_kit_modal .p-block_wrapper--modal_view:not(:first-of-type)),
// 4px for a context's and for a divider's after a context. App Home's
// have none (an input wrapper's 12px is in blockMargins).
func (s *Surface) wrapperTop(b *Block, afterContext bool) int {
	if s.isHome {
		return 0
	}
	if b.Type == "context" || b.Type == "divider" && afterContext {
		return 4
	}
	return 12
}

// richEdgeMargin is the margin a rich_text block's first (or last)
// element of type t gives the block's top (bottom): a code block's or a
// quote's 4px (.c-mrkdwn__pre, __quote), a divider's 8px
// (.p-rich_text_divider), a header's 4px under it (none above the
// block's first, as richview draws it).
func richEdgeMargin(t string, first bool) int {
	switch t {
	case "rich_text_preformatted", "rich_text_quote":
		return 4
	case "rich_text_divider":
		return 8
	case "rich_text_header":
		if !first {
			return 4
		}
	}
	return 0
}

func (s *Surface) block(gtx C, b *Block, i int, w int, st *richview.Style) int {
	th := s.h.Theme()
	cl := colorsOf(th)
	key := surfaceKey(b, i)
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
	// the block's outer margins are Layout's (blockMargins), to collapse
	// with its wrapper's as Slack's do
	o := richview.Options{FillBlocks: true, NoOuterMargins: ownsMargins(b.Type)}
	v := s.textWith(key, o, func() richtext.Doc { return s.docOf(b) })
	if len(b.Nested) > 0 || len(b.Elements) > 0 { // child blocks, a fallback's payload, a card's actions: live elements
		if s.nested == nil {
			s.nested = &nestedElements{s: s}
		}
		o.Elements = s.nested
		v.SetOptions(o)
	}
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

// flow lays out an actions block's elements in Slack's wrapping rows
// (flowRun).
func (s *Surface) flow(gtx C, b *Block, w int) int { return s.flowRun(gtx, b, w, true) }

// flowRun is flow; without draw it only measures (nestedElements).
//
// .p-actions_block_elements is a wrapping flex row (align-items: end) of
// actions with an 8px top and right margin (margin 8px 8px 0 0), the top
// one the gap between rows (Layout gives the first row its own). In a
// modal each action is calc(50% - 8px) wide, two to a row, its element
// filling it (.p-actions_block_wrapper--modal_view); in App Home it is
// its element's width. Checkboxes and radio buttons take the whole row
// (p-actions_block__action--full_width: width 100%) on both.
func (s *Surface) flowRun(gtx C, b *Block, w int, draw bool) int {
	x, y, rowH := 0, 0, 0
	gap := dp(gtx, 8)
	self := s.selfID()
	modal := s.modal()
	colW := max(w/2-gap, 0)
	type cell struct {
		ew *ew
		x  int
		sz image.Point
	}
	var row []cell
	place := func() {
		if !draw {
			return
		}
		for _, c := range row {
			at(gtx, image.Pt(c.x, y+rowH-c.sz.Y), func(gtx C) D { return c.ew.layout(fixed(gtx, c.sz), c.sz) })
		}
	}
	for i := range b.Elements {
		e := &b.Elements[i]
		if hiddenFrom(e, self) {
			continue
		}
		full := e.Type == "checkboxes" || e.Type == "radio_buttons"
		cw := w
		if modal && !full {
			cw = colW
		}
		ew := s.widget(b, e)
		sz := ew.size(gtx, cw, false)
		if sz.X == 0 {
			continue
		}
		if modal && !stacks(e) && !isTextInput(e.Type) && e.Type != "datetimepicker" {
			sz.X = cw // .p-actions_block__action>button{width:100%}
		} else {
			sz.X = min(sz.X, cw)
		}
		adv := sz.X + gap // the action's margin box
		switch {
		case full:
			adv = w
		case modal:
			adv = colW + gap
		}
		if len(row) > 0 && x+adv > w {
			place()
			y += rowH + gap
			x, rowH, row = 0, 0, row[:0]
		}
		row = append(row, cell{ew: ew, x: x, sz: sz})
		x += adv
		rowH = max(rowH, sz.Y)
	}
	place()
	return y + rowH
}

func (s *Surface) section(gtx C, b *Block, key string, w int, st *richview.Style) int {
	acc := b.Accessory
	if iconButtonHidden(acc, s.selfID()) { // Section (bzDH) draws the accessory; IconButton hides itself
		acc = nil
	}
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
		ew.pos = image.Pt(0, s.curY+y)
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
		v.SetOptions(richview.Options{Muted: true, Compact: true, FillBlocks: true})
		d := at(gtx, image.Pt(0, y+dp(gtx, 4)), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return v.Layout(gtx, styleFor(th, s.h.Images()), &s.hand)
		})
		y += dp(gtx, 4) + d.Size.Y
	}
	if s.errs[b.BlockID] == "" && enterHint(b) {
		// InputBlockContent's p-block_kit_input_block__enter_help_text
		at(gtx, image.Pt(0, y+dp(gtx, 4)), func(gtx C) D {
			// the enter icon, not a ⏎ glyph: no bundled font has one
			is := dp(gtx, 13)
			at(gtx, image.Pt(0, (dp(gtx, 18)-is)/2), func(gtx C) D { return icon(gtx, "enter", 13, cl.Hint) })
			x := is + dp(gtx, 4)
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
	if errors.Is(err, context.DeadlineExceeded) { // a 429's wait outlasting it too (slack.RateLimitWait)
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
