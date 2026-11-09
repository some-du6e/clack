package slackc

import (
	"image"
	"sort"
	"strconv"
	"sync"

	"gioui.org/io/key"
)

// Clicks on links Clack draws natively (message text: a channel, a
// member, a URL), for plugins that listen for clicks on the document and
// look at the link element (Slick's Quick Join holds a click on a channel
// link to see whether a second one follows). Slack's link is an element;
// Clack's is a box in a richview layout. While JS listens for clicks on
// the window or document, a click on a native link is dispatched at a
// stand-in element jsrt keeps for that link (the tag and attributes
// Slack's DOM gives it, outside <body>: connected, never drawn), and the
// link's native action becomes that element's activation behavior: it
// runs once a click on the element isn't canceled, the one Go sent or
// one a listener dispatches later (a replayed click). Without a
// listener, nothing is sent and the caller acts at once.

// NativeLink is a natively drawn link as Slack's DOM has it.
type NativeLink struct {
	Key   string            // the same link: the same element on every click
	Tag   string            // "a" unless set
	Attrs map[string]string // class, href, data-channel-id…
	Text  string            // its textContent
	Rect  image.Rectangle   // window px
}

var linkFns struct {
	sync.Mutex
	slots map[string]int64 // link key → Go func id (see fnSlot)
	order []string         // keys, oldest first
}

// maxLinkFns bounds the links whose activation JS can still call; jsrt
// keeps as many stand-in elements.
const maxLinkFns = 256

// ClickLink dispatches a click (clicks: its click count) at link when JS
// listens for clicks on the window or document, and reports whether it
// did; activate, the link's native action, then runs on the UI goroutine
// when a click on its element isn't canceled. False: nothing listens, so
// the caller activates now. scale is px per CSS px.
func ClickLink(l NativeLink, clicks int, mods key.Modifiers, scale float32, activate func()) bool {
	if !WantsGlobal("click") {
		return false
	}
	b := bridge()
	if b == nil {
		return false
	}
	if scale <= 0 {
		scale = 1
	}
	id := linkFn(l.Key, activate)
	c := l.Rect.Min.Add(l.Rect.Max).Div(2)
	ev := make([]byte, 0, 256)
	ev = append(ev, `{"type":"click","button":0,"buttons":0`...)
	ev = append(ev, `,"detail":`...)
	ev = strconv.AppendInt(ev, int64(max(clicks, 1)), 10)
	ev = appendNum(ev, "clientX", float32(c.X)/scale)
	ev = appendNum(ev, "clientY", float32(c.Y)/scale)
	ev = appendMods(ev, mods)
	ev = append(ev, `,"native":{"key":`...)
	ev = appendString(ev, l.Key)
	tag := l.Tag
	if tag == "" {
		tag = "a"
	}
	ev = append(ev, `,"tag":`...)
	ev = appendString(ev, tag)
	ev = append(ev, `,"text":`...)
	ev = appendString(ev, l.Text)
	ev = append(ev, `,"attrs":{`...)
	keys := make([]string, 0, len(l.Attrs))
	for k := range l.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			ev = append(ev, ',')
		}
		ev = appendString(ev, k)
		ev = append(ev, ':')
		ev = appendString(ev, l.Attrs[k])
	}
	ev = append(ev, `},"rect":[`...)
	r := l.Rect
	for i, v := range [4]int{r.Min.X, r.Min.Y, r.Dx(), r.Dy()} {
		if i > 0 {
			ev = append(ev, ',')
		}
		ev = strconv.AppendFloat(ev, float64(float32(v)/scale), 'f', -1, 32)
	}
	ev = append(ev, `],"activate":{"$gofn":`...)
	ev = strconv.AppendInt(ev, id, 10)
	ev = append(ev, `}}}`...)
	b.Dispatch(0, ev)
	return true
}

// linkFn gives key's newest activation a Go func id (the same id for the
// same link), dropping the oldest links past maxLinkFns.
func linkFn(key string, activate func()) int64 {
	linkFns.Lock()
	defer linkFns.Unlock()
	if linkFns.slots == nil {
		linkFns.slots = map[string]int64{}
	}
	if _, ok := linkFns.slots[key]; !ok {
		linkFns.order = append(linkFns.order, key)
		if len(linkFns.order) > maxLinkFns {
			old := linkFns.order[0]
			linkFns.order = linkFns.order[1:]
			releaseSlots(map[string]int64{old: linkFns.slots[old]})
			delete(linkFns.slots, old)
		}
	}
	return fnSlot(linkFns.slots, key, activate)
}
