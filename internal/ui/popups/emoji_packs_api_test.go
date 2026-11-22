package popups

// What the emoji pack tests (emoji_packs_test.go) read from the dialog,
// the store and the picker, in one file: the red run's tree swaps it for
// one over the dialog as it was before the port.

import (
	"image"
	"slices"

	"github.com/some-du6e/clack/internal/core"
)

// epkDialog is the Add emoji dialog under test.
type epkDialog struct{ s *emojiAddModal }

func epkOpen(h Host, tab int) (*epkDialog, *Modal) {
	s := newEmojiAdd(h, tab)
	return &epkDialog{s}, s.modal
}

// tabRect is tab i in the window: the header under the title bar.
func (d *epkDialog) tabRect(i int) image.Rectangle {
	return d.s.tabR[i].Add(d.s.modal.rect.Min).Add(image.Pt(28, 70))
}

func (d *epkDialog) tab() int    { return d.s.tab }
func (d *epkDialog) cards() int  { return len(d.s.packs.cards) }
func (d *epkDialog) height() int { return d.s.modal.rect.Dy() }

// card is pack id's card in body coordinates and in the window.
func (d *epkDialog) card(id string) (body, win image.Rectangle, ok bool) {
	c := d.s.packs.cards[id]
	if c == nil {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	return c.r, c.r.Add(d.s.modal.bodyOrg), true
}

func (d *epkDialog) soon() image.Rectangle { return d.s.packs.soonR }

// detail is the open pack's id and whether it was installed.
func (d *epkDialog) detail() (id string, installed, ok bool) {
	if p := d.s.packs.detail; p != nil {
		return p.pack.ID, p.installed, true
	}
	return "", false, false
}

// emojiRects are the details' emoji in body coordinates.
func (d *epkDialog) emojiRects() []image.Rectangle {
	p := d.s.packs.detail
	if p == nil {
		return nil
	}
	out := make([]image.Rectangle, len(p.btns))
	for i, b := range p.btns {
		out[i] = b.r
	}
	return out
}

func (d *epkDialog) addRect() image.Rectangle    { return d.s.packs.add.Rect() }
func (d *epkDialog) removeRect() image.Rectangle { return d.s.packs.remove.Rect() }

// epkPack is a pack as the store holds it.
type epkPack struct {
	ID        string
	Installed bool
	Local     []string // its local emoji's names, in order
}

func epkPacks(a *core.App) ([]epkPack, bool) {
	packs, fetched := a.EmojiPacks()
	out := make([]epkPack, 0, len(packs))
	for _, p := range packs {
		q := epkPack{ID: p.ID, Installed: p.Installed}
		for _, e := range p.LocalEmoji {
			q.Local = append(q.Local, e.Name)
		}
		out = append(out, q)
	}
	return out, fetched
}

func epkBadge(a *core.App) bool { return a.EmojiPackInputBadge() }
func epkClearBadge(a *core.App) { a.ClearEmojiPackInputBadge() }

// epkPicker is what a picker shows of the packs.
type epkPicker struct {
	Groups       []string // the packs' groups' headings
	First        []string // the first pack group's emoji
	CustomShares bool     // Custom holds one of them too
	BeforeCustom bool     // the first pack group's heading comes right before Custom's
	// CustomTab is the heading the Custom tab goes to ("" for none), and
	// CustomClick the heading a click on it scrolled the list to
	CustomTab, CustomClick string
}

// epkPickerOf opens a picker and reads it once its index and pack groups
// have landed (ok false while they haven't).
func epkPickerOf(h Host) func() (epkPicker, bool) {
	p := NewEmojiPicker(h, nil)
	return func() (epkPicker, bool) {
		drain()
		p.refreshIndex()
		if !p.full || len(p.packNames) == 0 {
			return epkPicker{}, false
		}
		var out epkPicker
		for i := range p.packNames {
			out.Groups = append(out.Groups, p.groupLabel(epGroupSearch+1+i))
		}
		ids := p.lists[epGroupSearch+1]
		for _, id := range ids {
			out.First = append(out.First, p.idx.names[id])
		}
		for _, id := range p.lists[epGroupCustom] {
			if slices.Contains(ids, id) {
				out.CustomShares = true
				break
			}
		}
		rows, heads, _ := epLayoutRows(nil, p.lists, false, 27, 1, 33, 10)
		label := func(i int) string {
			if i < 0 || i >= len(rows) || rows[i].n != 0 {
				return ""
			}
			return p.groupLabel(int(rows[i].list))
		}
		out.CustomTab = label(int(heads[epGroupCustom]))
		p.rowsDirty = true // clickTab works the headings out afresh
		p.clickTab(epGroupCustom)
		out.CustomClick = label(p.list.Position.First)
		if p.list.Position.First == 0 && heads[epGroupCustom] != 0 {
			out.CustomClick = "" // left at the top: the tab found no heading
		}
		for i, r := range rows {
			if r.n == 0 && int(r.list) == epGroupSearch+1 {
				// the pack group's rows run up to Custom's heading
				next := i + 1
				for next < len(rows) && rows[next].n > 0 {
					next++
				}
				out.BeforeCustom = next < len(rows) && rows[next].n == 0 && int(rows[next].list) == epGroupCustom
			}
		}
		return out, true
	}
}
