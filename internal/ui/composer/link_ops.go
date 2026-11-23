package composer

// The editor side of Texty's link edits: each one undo step, as Quill's
// history records an updateContents or a formatText.

// unlinkRange is formatText(range, {link: false, unlink: true}) with the
// caret at the range's end (onRemoveLink).
func (e *Editor) unlinkRange(lo, hi int) {
	e.unlinkRangeCaret(lo, hi, hi)
}

// unlinkRangeCaret is unlinkRange with the caret at caret (the edit-link
// popover's Remove puts it at the end of the selection the click left).
func (e *Editor) unlinkRangeCaret(lo, hi, caret int) {
	e.init()
	d := e.doc
	lo, hi = d.clamp(lo), d.clamp(hi)
	if hi <= lo {
		return
	}
	e.begin(edOther, true)
	d.SetLink(lo, hi, "")
	d.SetStyle(lo, hi, Unlink, true)
	caret = d.clamp(caret)
	e.anchor, e.caret = caret, caret
	e.commit()
}

// replaceRange is updateContents(retain(lo).delete(hi-lo).insert(text,
// a)): text replaces [lo, hi) with a's style and link. With caret < 0 the
// selection moves as Quill's shiftRange moves it through that delta (an
// api change: a position in or at the end of [lo, hi) lands at lo, one
// after it moves with the text); otherwise the caret goes to caret.
func (e *Editor) replaceRange(lo, hi int, text string, a Attr, caret int) {
	e.init()
	d := e.doc
	lo, hi = d.clamp(lo), d.clamp(hi)
	if hi < lo {
		return
	}
	e.begin(edOther, true)
	d.Delete(lo, hi)
	end := d.Insert(lo, text, Attr{Style: a.Style})
	if a.Link != "" {
		d.SetLink(lo, end, a.Link)
	}
	if caret >= 0 {
		caret = d.clamp(caret)
		e.anchor, e.caret = caret, caret
	} else {
		shift := func(p int) int {
			switch {
			case p > hi:
				return p - (hi - lo) + (end - lo)
			case p >= lo:
				return lo
			}
			return p
		}
		e.anchor, e.caret = shift(e.anchor), shift(e.caret)
	}
	e.commit()
}

// saveLink is ComposerLinkModal.saveLink over [lo, hi): with the text
// changed, a slug being edited or nothing selected, the range becomes text
// linked to url in the range's formats (getFormat without slackmention,
// slackslug and link); otherwise the range is linked where it is, embeds
// left as they are. Linking at a caret adds a space after the link. A
// slug's edit (isEditingSlug) whose text is still its URL becomes a slug
// again (convertLinksToUnprocessedSlugs, applyLinkSlugEdit: chat.slugifyUrl).
func (e *Editor) saveLink(lo, hi int, text, url string, didTextChange, editingSlug bool) {
	e.init()
	d := e.doc
	lo, hi = d.clamp(lo), d.clamp(hi)
	if hi < lo {
		lo, hi = hi, lo
	}
	e.begin(edOther, true)
	end := hi
	if editingSlug || didTextChange || hi == lo {
		st := e.typingAttr(lo, hi).Style &^ Unlink // unlink: false
		d.Delete(lo, hi)
		end = d.Insert(lo, text, Attr{Style: st})
		d.SetLink(lo, end, url)
	} else {
		d.SetLink(lo, hi, url)
		d.SetStyle(lo, hi, Unlink, false)
	}
	caret := end
	if hi == lo {
		caret = d.Insert(end, " ", Attr{})
	}
	e.anchor, e.caret = caret, caret
	e.commit()
	if editingSlug {
		e.notePastedLinks(lo, end)
	}
}

// insertPlainLinkText is ComposerLinkModal.insertPlainText: the Link
// field emptied and the text changed, the range becomes that text with no
// link or format, the caret after it.
func (e *Editor) insertPlainLinkText(lo, hi int, text string) {
	e.init()
	d := e.doc
	lo, hi = d.clamp(lo), d.clamp(hi)
	e.begin(edOther, true)
	d.Delete(lo, hi)
	end := d.Insert(lo, text, Attr{Style: Unlink}) // {unlink: true}
	e.anchor, e.caret = end, end
	e.commit()
}

// removeFormLink is ComposerLinkModal.removeLink: the Link field emptied
// with the text as it was, the range loses its link (a slug being edited
// its slackslug too: its label stays as text), the caret at its end.
func (e *Editor) removeFormLink(lo, hi int, editingSlug bool) {
	e.init()
	d := e.doc
	lo, hi = d.clamp(lo), d.clamp(hi)
	if hi <= lo {
		return
	}
	if a := d.Attrs[lo].Atom; editingSlug && hi-lo == 1 && a != nil {
		e.replaceRange(lo, hi, a.Text, Attr{Style: Unlink}, lo+len([]rune(a.Text)))
		return
	}
	e.unlinkRange(lo, hi)
}

// edRange is a range the link menus, the popover's Edit and the link
// dialog act on later, kept live: Quill's range is an index the editor's
// own changes move (an async slug's answer swaps a pasted URL for a
// one-rune slug while the dialog is open). It holds the document as it
// was; resolve maps the range through what changed since.
type edRange struct {
	snap   *Doc
	ver    uint64
	lo, hi int
}

// liveRange is [lo, hi) of the document now.
func (e *Editor) liveRange(lo, hi int) edRange {
	e.init()
	return edRange{snap: e.doc.Clone(), ver: e.version, lo: lo, hi: hi}
}

// resolve is r in the document now: the same runes, moved by any change
// before them; ok is false when a change touched the range itself (its
// target is gone, and the action does nothing).
func (e *Editor) resolve(r edRange) (lo, hi int, ok bool) {
	e.init()
	if r.snap == nil {
		return 0, 0, false
	}
	if e.version == r.ver {
		return r.lo, r.hi, r.hi <= e.doc.Len()
	}
	old, cur := r.snap, e.doc
	same := func(i, j int) bool {
		if old.Runes[i] != cur.Runes[j] {
			return false
		}
		a, b := old.Attrs[i], cur.Attrs[j]
		if a.Style != b.Style || a.Link != b.Link || (a.Atom == nil) != (b.Atom == nil) {
			return false
		}
		return a.Atom == nil || *a.Atom == *b.Atom
	}
	n := min(old.Len(), cur.Len())
	pre := 0
	for pre < n && same(pre, pre) {
		pre++
	}
	suf := 0
	for suf < n-pre && same(old.Len()-1-suf, cur.Len()-1-suf) {
		suf++
	}
	switch {
	case r.hi <= pre:
		return r.lo, r.hi, true // before every change
	case r.lo >= old.Len()-suf:
		d := cur.Len() - old.Len()
		return r.lo + d, r.hi + d, true // after them: moved with the text
	}
	return 0, 0, false
}
