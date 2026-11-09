package composer

// Link slugs, 1:1 with Slack's composer (gantry-v2 2026-11, TextySlugs;
// core/slugs.go has the protocol): pasting a Slack link whose text is
// the URL asks chat.slugifyUrl what it is, without blocking the editor.
// A message link becomes Slack's message slug (the "💬 karim in #clack"
// pill, sent as a message_mention element), a channel link a #channel
// mention, a profile link an @user mention; anything else stays a link.
// Typed links only autolink, as in Slack. A message link still waiting
// for its answer at send time is resolved from the message cache.

import (
	"context"
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
)

// messageAtomLabel is the slug's plain label (getMessageSlugAttributes;
// the composer's plainTextChannelName keeps "#" for private channels).
func messageAtomLabel(r AtomResolver, ch, author, url string) string {
	if r == nil {
		return firstNonEmpty(url, ch)
	}
	label, lock, pre, _ := richtext.MessageMentionLabel(r, ch, author)
	if lock {
		label = label[:pre] + "#" + label[pre:]
	}
	return label
}

// messageAtom is the slug for a resolved message link.
func messageAtom(r AtomResolver, s core.Slug, url string) *Atom {
	return &Atom{Kind: AtomMessage, ID: s.ID, Channel: s.ChannelID, Author: s.AuthorID, Thread: s.ThreadTS, URL: url,
		Text: messageAtomLabel(r, s.ChannelID, s.AuthorID, url)}
}

// slugAtom converts any resolved slug (nil: it stays a link).
func slugAtom(r AtomResolver, s core.Slug, url string) *Atom {
	switch s.Type {
	case "message":
		return messageAtom(r, s, url)
	case "channel":
		name := s.Name
		if r != nil {
			if n := r.ChannelName(s.ID); n != "" && n != s.ID {
				name = n
			}
		}
		if name == "" {
			return nil
		}
		if !s.IsIM && !s.IsMPIM { // formatChannelMentionAttributesForDelta
			name = rtEnsurePrefix(name, "#")
		}
		return &Atom{Kind: AtomChannel, ID: s.ID, Text: name}
	case "user":
		name := s.Name
		if r != nil {
			if n := r.UserName(s.ID); n != "" {
				name = n
			}
		}
		if name == "" {
			return nil
		}
		return &Atom{Kind: AtomUser, ID: s.ID, Text: rtEnsurePrefix(name, "@")}
	}
	return nil
}

// messageSlugDelta is the slackslug attribute of a message slug op.
func messageSlugDelta(a *Atom) map[string]any {
	m := map[string]any{"id": a.ID, "label": a.Text, "type": "message", "secondaryObjectId": a.Channel}
	if a.URL != "" {
		m["url"] = a.URL
	}
	if a.Author != "" {
		m["authorId"] = a.Author
	}
	if a.Thread != "" {
		m["tertiaryObjectId"] = a.Thread
	}
	return m
}

// paintSlugIcon draws a message slug's "message" icon before its label.
func (l *tlLayout) paintSlugIcon(gtx layout.Context, li int, it *tlItem, col color.NRGBA) {
	m := &l.m
	px := m.ppem.Round()
	x := int(l.vxf(li, it.x0+it.padL) + 0.5)
	y := l.lines[li].base - px + max(1, px/8)
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	g := gtx
	g.Metric = unit.Metric{PxPerDp: 1, PxPerSp: 1}
	g.Constraints = layout.Exact(image.Pt(px, px))
	icons.GetSlack("message")(g, unit.Dp(px), col)
}

// ---- paste ----

// edSlugs is an editor's pasted links waiting for chat.slugifyUrl.
type edSlugs struct {
	pending map[string]bool
	// onPaste, set by the composer, resolves the links (off the UI
	// goroutine) and hands the answers to applySlugs.
	onPaste func(urls []string)
}

// slugCandidates lists the Slack links in [lo, hi) whose text is the URL:
// bare URLs and links labelled with their own URL, outside code.
func (e *Editor) slugCandidates(lo, hi int) []rtURLMatch {
	d := e.doc
	if hi <= lo {
		return nil
	}
	rs := make([]rune, hi-lo)
	for i := range rs {
		a := d.Attrs[lo+i]
		if a.Atom != nil || a.Style&Code != 0 {
			continue
		}
		rs[i] = d.Runes[lo+i]
	}
	var out []rtURLMatch
	for _, m := range rtFindURLs(rs) {
		a, b := lo+m.start, lo+m.end
		if !core.IsSlackURL(m.url) || string(d.Runes[a:b]) != m.url {
			continue
		}
		if l := d.LineOf(a); l < len(d.Lines) && d.Lines[l].Kind == CodeBlock {
			continue
		}
		ok := true
		for i := a; i < b; i++ {
			if ln := d.Attrs[i].Link; ln != "" && ln != m.url {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, rtURLMatch{start: a, end: b, url: m.url})
		}
	}
	return out
}

// notePastedLinks starts resolving the Slack links a paste put in [lo, hi).
func (e *Editor) notePastedLinks(lo, hi int) {
	if e.slugs.onPaste == nil {
		return
	}
	var urls []string
	seen := map[string]bool{}
	for _, m := range e.slugCandidates(lo, hi) {
		if !seen[m.url] {
			seen[m.url] = true
			urls = append(urls, m.url)
		}
	}
	if len(urls) == 0 {
		return
	}
	if e.slugs.pending == nil {
		e.slugs.pending = map[string]bool{}
	}
	for _, u := range urls {
		e.slugs.pending[u] = true
	}
	e.slugs.onPaste(urls)
}

// applySlugs replaces the still-unchanged pasted links with their slugs
// (one undo step). Links that resolved to nothing stay links.
func (e *Editor) applySlugs(atoms map[string]*Atom, done []string) {
	e.init()
	for _, u := range done {
		if atoms[u] == nil {
			delete(e.slugs.pending, u)
		}
	}
	if !replaceSlugLinks(e.doc, atoms, e.slugs.pending, func(a, b, n int) {
		shift := func(p int) int {
			switch {
			case p >= b:
				return p - (b - a) + n
			case p > a:
				return a + n
			}
			return p
		}
		e.anchor, e.caret = shift(e.anchor), shift(e.caret)
	}, func() { e.begin(edOther, true) }) {
		return
	}
	for u := range atoms {
		delete(e.slugs.pending, u)
	}
	e.commit()
}

// replaceSlugLinks swaps each pending link in d for its atom, last first.
// moved reports each replacement (runes [a, b) became n runes); before
// runs once, ahead of the first change. It reports whether d changed.
func replaceSlugLinks(d *Doc, atoms map[string]*Atom, pending map[string]bool, moved func(a, b, n int), before func()) bool {
	e := &Editor{doc: d}
	ms := e.slugCandidates(0, d.Len())
	changed := false
	for i := len(ms) - 1; i >= 0; i-- {
		m := ms[i]
		at := atoms[m.url]
		if at == nil || !pending[m.url] {
			continue
		}
		if !changed && before != nil {
			before()
		}
		changed = true
		d.Delete(m.start, m.end)
		n := d.InsertAtom(m.start, at) - m.start
		if moved != nil {
			moved(m.start, m.end, n)
		}
	}
	return changed
}

// ---- composer glue ----

// bindSlugs wires the editor's pasted links to chat.slugifyUrl.
func (c *Composer) bindSlugs() {
	if c.api.a == nil {
		return
	}
	app, ch := c.api.a, c.ch
	c.ed.slugs.onPaste = func(urls []string) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			res, err := app.SlugifyURLs(ctx, urls, ch)
			c.later(func() {
				r := c.api.resolver()
				atoms := map[string]*Atom{}
				if err == nil {
					for u, s := range res {
						if at := slugAtom(r, s, u); at != nil {
							atoms[u] = at
						}
					}
				}
				c.ed.applySlugs(atoms, urls)
				c.h.Invalidate()
			})
		}()
	}
}

// resolvePendingSlugs is withUnprocessedMessageSlugsResolved: message
// links still waiting for chat.slugifyUrl become slugs if the message is
// cached, in the copy of the doc being sent.
func (c *Composer) resolvePendingSlugs(doc *Doc) {
	if c.api.a == nil || len(c.ed.slugs.pending) == 0 {
		return
	}
	r := c.api.resolver()
	atoms := map[string]*Atom{}
	for u := range c.ed.slugs.pending {
		if s, ok := c.api.a.CachedSlug(u); ok {
			atoms[u] = messageAtom(r, s, u)
		}
	}
	if len(atoms) > 0 {
		replaceSlugLinks(doc, atoms, c.ed.slugs.pending, nil, nil)
	}
}

// PasteText pastes s at the caret as a clipboard paste does (slugs
// included). For tests and automation.
func (c *Composer) PasteText(s string) {
	c.ed.paste(s)
	c.h.Invalidate()
}

// HasMessageSlug reports whether the draft holds a message slug.
func (c *Composer) HasMessageSlug() bool {
	for _, a := range c.ed.Doc().Attrs {
		if a.Atom != nil && a.Atom.Kind == AtomMessage {
			return true
		}
	}
	return false
}
