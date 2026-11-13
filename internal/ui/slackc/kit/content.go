package kit

// Content components: avatars, profile hover triggers, mrkdwn and Block
// Kit bodies (richview), the emoji menu trigger and the character counter.
// RichTextInput lives in richinput.go.

import (
	"encoding/json"
	"image"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func init() {
	add(avatar, "ConnectedBaseAvatar", "Avatar", "BaseAvatar")
	add(profileHoverTrigger, "ProfileHoverTrigger")
	add(mrkdwnElement, "MrkdwnElement")
	add(blocks, "Blocks")
	add(characterCount, "CharacterCount")
	add(emojiMenuTrigger, "Connect(EmojiMenuTrigger)", "EmojiMenuTrigger")
}

// hoverDelay is how long the pointer rests before a hover card opens.
const hoverDelay = 500 * time.Millisecond

// ---- pointer tracking (hover, click, origin) ----

// ctPointer tracks hover and primary clicks over a widget and feeds the
// widget's origin tracker from the same events.
type ctPointer struct {
	tag     int
	hovered bool
	pressed bool
	clicked bool // a primary click completed since the last update
	since   time.Time
	fired   bool // the hover action ran for this hover
}

// update reads last frame's events; call before register.
func (p *ctPointer) update(gtx layout.Context, org *origin) {
	p.clicked = false
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &p.tag, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Press | pointer.Release | pointer.Cancel | pointer.Drag})
		if !ok {
			break
		}
		pe, ok := e.(pointer.Event)
		if !ok {
			continue
		}
		if org != nil && pe.Kind != pointer.Leave && pe.Kind != pointer.Cancel {
			org.learn(pe.Position.X, pe.Position.Y)
		}
		switch pe.Kind {
		case pointer.Enter, pointer.Move:
			if !p.hovered {
				p.hovered, p.since, p.fired = true, gtx.Now, false
			}
		case pointer.Leave, pointer.Cancel:
			p.hovered, p.pressed = false, false
		case pointer.Press:
			if pe.Buttons == pointer.ButtonPrimary || pe.Buttons == 0 {
				p.pressed = true
			}
		case pointer.Release:
			if p.pressed && p.hovered {
				p.clicked = true
			}
			p.pressed = false
		}
	}
}

// register adds the handler over sz; pass lets children below get the
// events too (wrappers), hand shows the pointer cursor.
func (p *ctPointer) register(gtx layout.Context, sz image.Point, pass, hand bool) {
	if pass {
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	}
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &p.tag)
	if hand {
		pointer.CursorPointer.Add(gtx.Ops)
	}
}

// hoverDue reports, once per hover, that the pointer has rested for d
// (scheduling a frame for when it will).
func (p *ctPointer) hoverDue(gtx layout.Context, d time.Duration) bool {
	if !p.hovered || p.fired {
		return false
	}
	due := p.since.Add(d)
	if gtx.Now.Before(due) {
		gtx.Execute(op.InvalidateCmd{At: due})
		return false
	}
	p.fired = true
	return true
}

// ---- Avatar ----

type avatarState struct {
	ptr  ctPointer
	org  origin
	tip  popups.TipState
	card bool
}

// iconKeys are Slack's image set keys, smallest first.
var iconKeys = []int{24, 32, 34, 36, 44, 48, 64, 68, 72, 88, 102, 132, 192, 230, 512, 1024}

// pickIcon picks the smallest image_N in an icons/profile object that is
// at least px wide (the largest otherwise); "" when there is none.
func pickIcon(icons slackc.Props, px int) string {
	if len(icons) == 0 {
		return ""
	}
	best, largest, largestN := "", "", 0
	for _, n := range iconKeys {
		u, _ := icons["image_"+strconv.Itoa(n)].(string)
		if u == "" {
			continue
		}
		if n >= px && best == "" {
			best = u
		}
		if n > largestN {
			largest, largestN = u, n
		}
	}
	if best != "" {
		return best
	}
	if largest != "" {
		return largest
	}
	for _, k := range []string{"image_original", "image"} {
		if u, _ := icons[k].(string); u != "" {
			return u
		}
	}
	return ""
}

// avatarRadius is Slack's clamp(4px, min(22.222%, 12px), 12px) (px).
func avatarRadius(px int) int {
	r := int(float32(px)*0.22222 + 0.5)
	if r < 4 {
		r = 4
	}
	if r > 12 {
		r = 12
	}
	return r
}

// avatarSource resolves the image URL and display name for avatar props.
func avatarSource(p slackc.Props, px int) (url, name string) {
	if u := pickIcon(p.Map("icons"), px); u != "" {
		url = u
	}
	c := envOf().Core
	if id := p.StrOr("userId", ""); id != "" {
		if c != nil {
			if u, ok := c.User(id); ok {
				name = u.RealName
				if name == "" {
					name = u.Name
				}
				if url == "" {
					url = u.Avatar
					if px > 72 && u.Avatar192 != "" {
						url = u.Avatar192
					}
				}
			} // else the lookup asked for the member: "names" draws it again
		}
		return url, name
	}
	if id := p.StrOr("botId", ""); id != "" {
		bp := p.Map("botProfile")
		name = bp.StrOr("name", "")
		if c != nil && c.S != nil {
			if b, ok := c.S.Bot(id); ok {
				if b.Name != "" {
					name = b.Name
				}
				if url == "" {
					url = b.Icon
				}
			}
		}
		if url == "" {
			url = pickIcon(bp.Map("icons"), px)
		}
	}
	return url, name
}

func avatar(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[avatarState](h)
	th := thm()
	cl := colorsOf(th)
	p := h.Props
	size := float32(36)
	if f, ok := p.Num("size"); ok && f > 0 {
		size = float32(f)
	}
	px := dp(gtx, size)
	sz := image.Pt(px, px)
	url, name := avatarSource(p, px)
	userID := p.StrOr("userId", "")
	interactive := !p.Has("isInteractive") || p.Bool("isInteractive")

	st.ptr.update(gtx, &st.org)
	open := func(f func(image.Point, string)) {
		if userID != "" && f != nil {
			f(st.org.toWin(image.Pt(px+dp(gtx, 8), 0)), userID)
		}
	}
	// BaseAvatar (client-boot xc3R): a click calls openProfile (the
	// profile pane); the card is the hover card, with showCard on an
	// interactive avatar of 36px or more
	if interactive && st.ptr.clicked {
		open(envOf().OpenProfile)
	}
	if interactive && size >= 36 && p.Bool("showCard") && st.ptr.hoverDue(gtx, hoverDelay) {
		open(envOf().OpenProfileCard)
	}

	r := image.Rectangle{Max: sz}
	drawn := false
	if url != "" {
		if c := envOf().Images; c != nil {
			if img, ok := c.Get(url, px); ok {
				richview.DrawImage(gtx.Ops, img, r, avatarRadius(px), true)
				drawn = true
			}
		}
	}
	if !drawn {
		theme.RRect(gtx, r, avatarRadius(px), cl.Skeleton)
	}
	st.ptr.register(gtx, sz, !interactive, interactive && userID != "")
	if p.Bool("showTooltip") && name != "" {
		popups.Tooltip(gtx, th, &st.tip, st.ptr.hovered, sz, popups.Above, name, "")
	}
	return layout.Dimensions{Size: sz}
}

// ---- ProfileHoverTrigger ----

type hoverTriggerState struct {
	ptr ctPointer
	org origin
}

func profileHoverTrigger(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[hoverTriggerState](h)
	st.ptr.update(gtx, &st.org)
	d := h.Children(gtx)
	id := h.Props.StrOr("memberId", "")
	if id != "" {
		side, _ := position(h.Props.StrOr("position", ""), popups.Below)
		open := func() {
			f := envOf().OpenProfileCard
			if f == nil {
				return
			}
			var a image.Point
			switch side {
			case popups.Right:
				a = image.Pt(d.Size.X+dp(gtx, 8), 0)
			case popups.Above:
				a = image.Pt(0, -dp(gtx, 4))
			default:
				a = image.Pt(0, d.Size.Y+dp(gtx, 4))
			}
			f(st.org.toWin(a), id)
		}
		// HoverCardTrigger (boot-shared 0oEe) opens on hover only; a click
		// goes to the children's own handlers (openOnClick is off)
		if st.ptr.hoverDue(gtx, hoverDelay) {
			open()
		}
	}
	st.ptr.register(gtx, d.Size, true, false)
	return d
}

// ---- rich text (richview) ----

type rvStyleEntry struct {
	st   *richview.Style
	dark bool
	gen  uint64 // th.Gen the palette was set at (stylesheet tokens)
}

var (
	rvMu     sync.Mutex
	rvStyles = map[*text.Shaper]*rvStyleEntry{}
)

// richStyle is the richview.Style for th's shaper, like
// messages.styleFor: Lato, theme emoji, palette and embolden per mode.
func richStyle(th *theme.Theme) *richview.Style {
	rvMu.Lock()
	defer rvMu.Unlock()
	e, ok := rvStyles[th.Shaper]
	if !ok {
		var imgs richview.Images
		if c := envOf().Images; c != nil {
			imgs = c
		}
		e = &rvStyleEntry{st: richview.NewStyle(th.Shaper, imgs, richview.LightPalette())}
		e.st.Face = theme.Typeface
		e.st.EmojiURL = th.EmojiURL
		e.dark = !th.Dark // force the palette below
		rvStyles[th.Shaper] = e
	}
	if e.dark != th.Dark || e.gen != th.Gen {
		e.dark, e.gen = th.Dark, th.Gen
		if th.Dark {
			e.st.SetPalette(richview.DarkPalette())
			e.st.Embolden = theme.BodyEmboldenFor(theme.DarkPalette.Fg)
		} else {
			e.st.SetPalette(richview.LightPalette())
			e.st.Embolden = theme.BodyEmboldenFor(theme.LightPalette.Fg)
		}
		if p, ok := richview.Themed(th); ok {
			e.st.SetPalette(p)
		}
	}
	return e.st
}

// richHandler routes richview clicks (anchors are window coordinates:
// the views' Origin is set from the origin tracker).
type richHandler struct{}

func (richHandler) OpenURL(u string) { openURL(u) }
func (richHandler) OpenUser(id string, anchor image.Point) {
	if f := envOf().OpenProfile; f != nil {
		f(anchor, id)
	}
}
func (richHandler) OpenChannel(id string)                       { navigate("c/" + id) }
func (richHandler) OpenUsergroup(id string, anchor image.Point) {}
func (richHandler) OpenImage(img richtext.Image)                { openURL(img.URL) }
func (richHandler) OpenFile(f *richtext.File) {
	if f == nil {
		return
	}
	switch {
	case f.Permalink != "":
		openURL(f.Permalink)
	case f.URL != "":
		openURL(f.URL)
	default:
		openURL(f.Download)
	}
}
func (richHandler) Button(b richtext.Button) {}

// nopResolver names nothing (no Core yet: tests, early frames).
type nopResolver struct{}

func (nopResolver) UserName(id string) string                     { return id }
func (nopResolver) ChannelName(id string) string                  { return id }
func (nopResolver) UsergroupName(id string) string                { return id }
func (nopResolver) Emoji(name string) (url, text string, ok bool) { return "", "", false }
func (nopResolver) SelfID() string                                { return "" }

// resolver is the store (which names users, channels and emoji), or a
// no-op one before Core exists.
func resolver() richtext.Resolver {
	if c := envOf().Core; c != nil && c.S != nil {
		return c.S
	}
	return nopResolver{}
}

type richState struct {
	view *richview.View
	key  string
	ptr  uintptr // identity of the last msg map (Blocks)
	org  origin
}

// layoutDoc draws doc with the instance's richview.View (re-Set when key
// changes) and tracks the origin for click anchors.
func (s *richState) layoutDoc(gtx layout.Context, key string, doc func() richtext.Doc) layout.Dimensions {
	if s.view == nil {
		s.view = richview.New(doc(), richview.Options{})
		s.key = key
	} else if s.key != key {
		s.view.Set(doc(), richview.Options{})
		s.key = key
	}
	s.view.Origin = s.org.get()
	d := s.view.Layout(gtx, richStyle(thm()), richHandler{})
	s.org.track(gtx, image.Rectangle{Max: d.Size})
	return d
}

// ---- MrkdwnElement ----

func mrkdwnElement(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[richState](h)
	txt := valueText(h.Props["text"])
	return st.layoutDoc(gtx, txt, func() richtext.Doc {
		if c := envOf().Core; c != nil && c.S != nil {
			return richtext.Doc{Blocks: c.Mrkdwn(txt)}
		}
		return richtext.Doc{Blocks: richtext.FromMrkdwn(nopResolver{}, txt)}
	})
}

// ---- Blocks ----

// jsonClean turns props into plain JSON values: Props → map, functions
// and elements dropped (they can't be part of a Slack message).
func jsonClean(v any) (any, bool) {
	switch v := v.(type) {
	case nil, string, bool, float64, float32, int, int64, int32:
		return v, true
	case slackc.JSFunc, slackc.Element:
		return nil, false
	case slackc.Props:
		return cleanMap(v), true
	case map[string]any:
		return cleanMap(v), true
	case []any:
		out := make([]any, 0, len(v))
		for _, x := range v {
			if c, ok := jsonClean(x); ok {
				out = append(out, c)
			}
		}
		return out, true
	case json.RawMessage:
		return v, true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Func, reflect.Chan:
		return nil, false
	case reflect.Slice, reflect.Array:
		out := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			if c, ok := jsonClean(rv.Index(i).Interface()); ok {
				out = append(out, c)
			}
		}
		return out, true
	}
	return v, true
}

func cleanMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, x := range m {
		if c, ok := jsonClean(x); ok {
			out[k] = c
		}
	}
	return out
}

// messageOf builds a slack.Message from Blocks' msg prop.
func messageOf(msg slackc.Props) (slack.Message, string) {
	var m slack.Message
	if msg == nil {
		return m, ""
	}
	c, _ := jsonClean(msg)
	b, err := json.Marshal(c)
	if err != nil {
		return m, ""
	}
	_ = json.Unmarshal(b, &m)
	return m, string(b)
}

func mapID(m slackc.Props) uintptr {
	if m == nil {
		return 0
	}
	return reflect.ValueOf(m).Pointer()
}

func blocks(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[richState](h)
	msg := h.Props.Map("msg")
	key := st.key
	var m slack.Message
	built := false
	if id := mapID(msg); id != st.ptr || st.view == nil {
		st.ptr = id
		m, key = messageOf(msg)
		built = true
	}
	return st.layoutDoc(gtx, key, func() richtext.Doc {
		if !built {
			m, _ = messageOf(msg)
		}
		return richtext.FromMessage(resolver(), &m)
	})
}

// ---- CharacterCount ----

func characterCount(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	limit := h.Props.IntOr("maxCharacterLimit", 0)
	count := h.Props.IntOr("currentCount", 0)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(h.Children),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return limitLine(gtx, limit, count)
		}),
	)
}

// overLimit reports whether count exceeds a positive limit.
func overLimit(limit, count int) bool { return limit > 0 && count > limit }

// limitLine draws Slack's "N characters maximum" error under a field when
// count is over limit.
func limitLine(gtx layout.Context, limit, count int) layout.Dimensions {
	if !overLimit(limit, count) {
		return layout.Dimensions{}
	}
	th := thm()
	cl := colorsOf(th)
	return layout.Inset{Top: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return drawIcon(gtx, "warning", 16, cl.Error)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 13, 18, theme.Regular, cl.Error, strconv.Itoa(limit)+" characters maximum", 0)
			}),
		)
	})
}

// ---- EmojiMenuTrigger ----

type emojiTriggerState struct {
	ptr ctPointer
	org origin
}

// emojiPicked is the onEmojiSelected payload (emoji object, name).
func emojiPicked(name string) []any {
	return []any{slackc.Props{"name": name, "id": name, "colons": ":" + name + ":"}, name}
}

func emojiMenuTrigger(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := stateOf[emojiTriggerState](h)
	st.ptr.update(gtx, &st.org)
	d := h.Children(gtx)
	if st.ptr.clicked {
		pos := strings.ToLower(h.Props.StrOr("position", "top"))
		offY := 0
		if f, ok := h.Props.Num("offsetY"); ok {
			offY = dp(gtx, float32(f))
		}
		props := h.Props
		pick := func(name string) {
			props.Invoke("onEmojiSelected", emojiPicked(name)...)
		}
		if strings.HasPrefix(pos, "bottom") {
			p := popups.NewEmojiPickerAt(ph, st.org.toWin(image.Pt(0, d.Size.Y+offY)), pick)
			p.SetSide(popups.Below)
			showOverlay(p)
		} else if f := envOf().OpenEmojiPicker; f != nil {
			f(st.org.toWin(image.Pt(0, -offY)), pick)
		}
	}
	st.ptr.register(gtx, d.Size, true, true)
	return d
}
