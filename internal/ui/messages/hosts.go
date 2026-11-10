package messages

import (
	"image"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// RegisterSlackHosts registers the message widgets Taut plugins render
// themselves ({"t":"@"} host nodes): api.elements.Avatar /
// ConnectedBaseAvatar, Blocks / MessageBlocks, MrkdwnElement, MemberLink
// and the BaseMrkdwnChannel pill. Call once, before plugins run.
func RegisterSlackHosts() {
	slackc.RegisterHost(hostAvatar, "ConnectedBaseAvatar", "Avatar")
	slackc.RegisterHost(hostBlocks, "Blocks", "MessageBlocks")
	slackc.RegisterHost(hostMrkdwn, "MrkdwnElement")
	slackc.RegisterHost(hostMemberLink, "MemberLink")
	slackc.RegisterHost(hostChannel, "BaseMrkdwnChannel")
}

func hostCore() *core.App {
	if lastHost == nil {
		return nil
	}
	return lastHost.Core()
}

// hostAnchor is a popup anchor for a click in a host component.
func hostAnchor(image.Point) image.Point {
	if ph, ok := lastHost.(PointerHost); ok {
		return ph.PointerPos()
	}
	return image.Point{}
}

// hostAvatar is ConnectedBaseAvatar {userId, botId, botProfile, size,
// isInteractive}: a size×size (CSS px) avatar that opens the profile.
func hostAvatar(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	p := h.Props
	s := gtx.Dp(unit.Dp(p.IntOr("size", 36)))
	size := image.Pt(s, s)
	id := p.StrOr("userId", "")
	url := avatarURL(lastHost, p, "", "")
	c := colorsFor(h.Theme())
	btn := h.State(func() any { return new(widget.Clickable) }).(*widget.Clickable)
	if (!p.Has("isInteractive") || p.Bool("isInteractive")) && id != "" && lastHost != nil {
		if btn.Clicked(gtx) {
			lastHost.OpenProfile(hostAnchor(image.Point{}), id)
		}
		gtx.Constraints = layout.Exact(size)
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			paintAvatar(gtx, h.Images(), url, image.Rectangle{Max: size}, c)
			return layout.Dimensions{Size: size}
		})
	}
	paintAvatar(gtx, h.Images(), url, image.Rectangle{Max: size}, c)
	return layout.Dimensions{Size: size}
}

// docHost is a host component drawing a richview Doc.
type docHost struct {
	key     string
	id      uintptr // propsID of the props the doc came from
	v       *richview.View
	handler richHandler
	msg     core.Message
}

func docState(h *slackc.Host) *docHost {
	return h.State(func() any { return &docHost{} }).(*docHost)
}

// set shows doc (built by mk) when key or id changed.
func (d *docHost) set(key string, id uintptr, opts richview.Options, mk func() richtext.Doc) {
	if d.v != nil && d.key == key && d.id == id {
		return
	}
	d.key, d.id = key, id
	if d.v == nil {
		d.v = richview.New(mk(), opts)
	} else {
		d.v.Set(mk(), opts)
	}
}

func (d *docHost) layout(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	st := styleForTheme(h.Theme(), h.Images())
	if lastHost == nil {
		return d.v.Layout(gtx, st, nopHandler{})
	}
	d.handler = richHandler{h: lastHost, m: &d.msg, anchor: hostAnchor}
	return d.v.Layout(gtx, st, &d.handler)
}

// hostMrkdwn is MrkdwnElement {text}: mrkdwn → richtext → richview, with
// Slack's .p-mrkdwn_element class.
func hostMrkdwn(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	text := h.Props.StrOr("text", "")
	d := docState(h)
	d.set(text, 0, richview.Options{}, func() richtext.Doc {
		if app := hostCore(); app != nil {
			return richtext.Doc{Blocks: app.Mrkdwn(text)}
		}
		return richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Text: text}}}}}
	})
	st := mrkdwnClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	return d.layout(gtx, h)
}

// hostBlocks is Blocks / MessageBlocks {msg}: the message's blocks (and
// text fallback, files, attachments) as Clack draws a message body.
func hostBlocks(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	msg := h.Props.Map("msg")
	d := docState(h)
	d.msg.TS, d.msg.Channel = msg.StrOr("ts", ""), msg.StrOr("channel", "")
	d.set("", propsID(msg), richview.Options{}, func() richtext.Doc { return parseMessage(hostCore(), msg) })
	st := blocksClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	return d.layout(gtx, h)
}

// hostMemberLink is MemberLink {id, className, children}: the member's
// name (or the children a plugin passed) as a link to their profile.
func hostMemberLink(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	id := h.Props.StrOr("id", "")
	st := memberLinkClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	btn := h.State(func() any { return new(widget.Clickable) }).(*widget.Clickable)
	if btn.Clicked(gtx) && id != "" && lastHost != nil {
		lastHost.OpenProfile(hostAnchor(image.Point{}), id)
	}
	if h.HasChildren() {
		return btn.Layout(gtx, h.Children)
	}
	name := id
	if app := hostCore(); app != nil {
		if u, ok := app.User(id); ok && u.Name != "" {
			name = u.Name
		} else if !ok && lastHost != nil {
			ensureUser(lastHost, id)
		}
	}
	th := h.Theme()
	c := colorsFor(th)
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return label(gtx, th, textStyle{size: float32(st.FontSize(15)), weight: theme.Bold, col: st.Color(c.Link)}, name)
	})
}

// hostChannel is BaseMrkdwnChannel {id, channelName}: the #channel pill.
func hostChannel(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	p := h.Props
	id := p.StrOr("id", "")
	name := p.StrOr("channelName", "")
	if name == "" {
		if app := hostCore(); app != nil {
			if cv, ok := app.Conv(id); ok {
				name = cv.Name
			}
		}
	}
	if name == "" {
		name = id
	}
	d := docState(h)
	d.set(id+"\x00"+name, 0, richview.Options{}, func() richtext.Doc {
		return richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Channel, ID: id, Text: "#" + name}}}}}
	})
	st := channelEntityClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	return d.layout(gtx, h)
}

// nopHandler drops clicks (no Host yet).
type nopHandler struct{}

func (nopHandler) OpenURL(string)                    {}
func (nopHandler) OpenUser(string, image.Point)      {}
func (nopHandler) OpenChannel(string)                {}
func (nopHandler) OpenUsergroup(string, image.Point) {}
func (nopHandler) OpenImage(richtext.Image)          {}
func (nopHandler) OpenFile(*richtext.File)           {}
func (nopHandler) Button(richtext.Button)            {}
