package messages

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
)

// Channel mentions of conversations the store doesn't know (not in the
// sidebar, never seen) arrive as "#C0123…". They are resolved once with
// conversations.info (core.ChannelInfo) in the background, and rows
// re-render with the name.
//
// A name shows from the frame whose users revision announces it (chanEntry
// at), for every row at once: one stored while a frame was drawing
// reached the rows laid out after it and not those before, which kept
// the raw id, wrapped differently and fixed themselves a frame later (a
// row 22px off for a frame, then the list jumped; CI run 42663172766).

type chanEntry struct {
	name string // "" while pending or unknown
	at   uint64 // the hub users revision that shows it
}

var (
	chanMu    sync.Mutex
	chanNames = map[string]chanEntry{}
	// chanFrame is the users revision this frame's rows resolve with (set
	// by layoutList; UI goroutine): names announced after it wait.
	chanFrame uint64
)

// channelName returns the display name for id, starting a lookup if needed.
func channelName(h Host, id string) (string, bool) {
	chanMu.Lock()
	e, asked := chanNames[id]
	chanMu.Unlock()
	if asked {
		// ours to announce, even once the store has the conversation
		// (ChannelInfo stores it before the lookup returns)
		if e.name != "" && e.at <= chanFrame {
			return e.name, true
		}
		return "", false
	}
	app := h.Core()
	if app == nil {
		return "", false
	}
	if r := app.ConvRef(id); r.Name != "" && r.Name != id {
		return r.Name, true
	}
	chanMu.Lock()
	chanNames[id] = chanEntry{}
	chanMu.Unlock()
	hb := hubFor(h)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		d, err := app.ChannelInfo(ctx, id)
		name := d.Conv.Name
		if err != nil && strings.Contains(err.Error(), "not_found") {
			// the viewer can't see it: Slack's "Private channel" chip
			// (richtext channel.go), not a made-up name
			app.S.SetChannelAccess(id, richtext.ChannelHidden)
			name = hiddenChannel
		}
		if name == "" {
			return
		}
		announceChannel(hb, id, name)
	}()
	return "", false
}

// announceChannel stores a looked-up name for the next users revision and
// then makes that revision: no frame sees the name before its rows are
// told to resolve again.
func announceChannel(hb *hub, id, name string) {
	hb.mu.Lock()
	at := hb.userRv + 1
	hb.mu.Unlock()
	chanMu.Lock()
	chanNames[id] = chanEntry{name: name, at: at}
	chanMu.Unlock()
	hb.mu.Lock()
	hb.userRv++
	hb.mu.Unlock()
	hb.invalidate()
}

// hiddenChannel marks a channel conversations.info doesn't show the viewer.
const hiddenChannel = "\x00hidden"

func rawChannel(sp *richtext.Span) bool {
	return sp.Kind == richtext.Channel && sp.ID != "" && (sp.Text == "#"+sp.ID || sp.Text == sp.ID || sp.Text == "")
}

func spansNeedChan(ss []richtext.Span) bool {
	for i := range ss {
		if rawChannel(&ss[i]) {
			return true
		}
	}
	return false
}

func blocksNeedChan(bs []richtext.Block) bool {
	for i := range bs {
		b := &bs[i]
		if spansNeedChan(b.Spans) || blocksNeedChan(b.Children) {
			return true
		}
		for _, it := range b.Items {
			if spansNeedChan(it) {
				return true
			}
		}
		for _, f := range b.Fields {
			if spansNeedChan(f) {
				return true
			}
		}
		if a := b.Att; a != nil && (spansNeedChan(a.Pretext) || spansNeedChan(a.Title) || spansNeedChan(a.Footer) || blocksNeedChan(a.Text)) {
			return true
		}
	}
	return false
}

// fixChannels returns a copy of bs with known channel names filled in.
func fixChannels(h Host, bs []richtext.Block) []richtext.Block {
	fs := func(ss []richtext.Span) []richtext.Span {
		if !spansNeedChan(ss) {
			return ss
		}
		out := append([]richtext.Span(nil), ss...)
		for i := range out {
			if rawChannel(&out[i]) {
				if n, ok := channelName(h, out[i].ID); ok && n == hiddenChannel {
					out[i].Text, out[i].Off, out[i].Lock = richtext.PrivateChannelLabel, true, true
				} else if ok {
					out[i].Text = "#" + n
				}
			}
		}
		return out
	}
	fss := func(sss [][]richtext.Span) [][]richtext.Span {
		if sss == nil {
			return nil
		}
		out := make([][]richtext.Span, len(sss))
		for i := range sss {
			out[i] = fs(sss[i])
		}
		return out
	}
	out := make([]richtext.Block, len(bs))
	for i := range bs {
		b := bs[i]
		b.Spans = fs(b.Spans)
		b.Items = fss(b.Items)
		b.Fields = fss(b.Fields)
		if len(b.Children) > 0 {
			b.Children = fixChannels(h, b.Children)
		}
		if b.Att != nil {
			a := *b.Att
			a.Pretext, a.Title, a.Footer = fs(a.Pretext), fs(a.Title), fs(a.Footer)
			if len(a.Text) > 0 {
				a.Text = fixChannels(h, a.Text)
			}
			b.Att = &a
		}
		out[i] = b
	}
	return out
}
