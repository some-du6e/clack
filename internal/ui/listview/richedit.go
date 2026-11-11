package listview

// Formatted text cells. Slack's text cell editor is a rich text editor
// (modern.vendor W077 under 3VkJ BaseFieldInputRenderer), so a cell with
// bold or italic text, links with names, mentions, channels, emoji,
// lists, quotes or code edits in place like any other. Clack's inline
// editor is plain text, so those cells open in the composer's rich editor
// instead, and save their rich_text as the editor writes it
// (composer.Doc.RichTextBlocks, the serializer messages use). A cell opens
// only when the editor reads it and writes it back the same
// (richRoundTrips); what it can't hold, a date for one, keeps the guard's
// "can't be edited in Clack yet". Enter saves, Escape cancels and leaving
// the cell saves, as in the plain editor.

import (
	"encoding/json"
	"errors"
	"image"
	"sort"
	"strconv"
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// startRich opens rec's text cell c in the rich editor when the plain
// editor refused it for its formatting (err) and the rich editor holds it
// exactly. typed is a key that started the edit ("" for none).
func (k *ctl) startRich(d *core.ListDoc, rec *lists.Record, c *lists.Column, f *lists.Field, err error, typed string) bool {
	var u lists.Unsupported
	if d == nil || !d.Info.Editable || rec == nil || c == nil || f == nil || !errors.As(err, &u) ||
		(c.Type != lists.Text && c.Type != lists.RichText) || !richRoundTrips(f.RichText) {
		return false
	}
	var r composer.AtomResolver
	if a := k.h.Core(); a != nil && a.S != nil {
		r = a.S
	}
	doc, perr := composer.DocFromBlocksJSON(f.RichText, r)
	if perr != nil {
		return false
	}
	ed := composer.NewEditor()
	ed.SetDoc(doc)
	n := doc.Len()
	ed.SetSelection(n, n) // setCursorAtEnd
	if typed != "" {
		ed.InsertText(typed)
	}
	e := &k.edit
	e.active, e.rec, e.col, e.focus, e.focused = true, rec.ID, c, true, false
	e.groupCol, e.group, e.done = nil, "", nil
	e.rich, e.richOrig = ed, append(json.RawMessage(nil), f.RichText...)
	e.original = ""
	return true
}

// layoutRich draws the rich editor in the cell (the editing border of the
// plain editor) and closes it like the plain one.
func (k *ctl) layoutRich(gtx layout.Context, d *core.ListDoc, w, h int) layout.Dimensions {
	e := &k.edit
	th := k.h.Theme()
	c := &th.C
	finish := func(how int) layout.Dimensions {
		done := e.done
		e.done = nil
		if how == doneEscape {
			e.active, e.rich = false, nil
		} else {
			k.commit(d)
		}
		if done != nil {
			done(how)
		}
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	ed := e.rich
	shortcut.OwnEscape(ed) // the editor is its own focus tag
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		switch ev.Kind {
		case composer.EvSubmit:
			return finish(doneEnter)
		case composer.EvCancel:
			return finish(doneEscape)
		}
	}
	if e.focus {
		ed.Focus(gtx)
		e.focus = false
	} else if ed.Focused(gtx) {
		e.focused = true
	} else if e.focused {
		return finish(doneBlur)
	}
	r := image.Rect(0, 0, w, h)
	theme.Fill(gtx, r, c.Bg)
	theme.Border(gtx, r, gtx.Dp(2), gtx.Dp(2), shade(th, "lagoon-50"))
	st := composer.DefaultEditorStyle(th.Shaper)
	st.Color, st.Caret, st.Selection, st.Placeholder = c.Fg, c.Fg, c.Selection, c.FgSubtle
	st.LinkColor, st.CodeColor, st.CodeBg, st.CodeBorder = c.Link, c.CodeFg, c.CodeBg, c.CodeBorder
	st.PreBg, st.PreBorder, st.QuoteBar = c.PreBg, c.CodeBorder, c.QuoteBar
	st.MentionColor, st.MentionBg, st.BroadcastColor, st.BroadcastBg = c.Mention, c.MentionBg, c.Fg, c.SelfMention
	st.PlaceholderText, st.EmojiSize = "", 20
	if im := k.h.Images(); im != nil {
		st.Images = im.Get
	}
	pad := gtx.Dp(10)
	ew := max(1, w-2*pad)
	eh := max(1, h-gtx.Dp(4))
	at(gtx, image.Pt(pad, gtx.Dp(2)), image.Pt(ew, eh), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		return vcenter(gtx, eh, func(gtx layout.Context) layout.Dimensions { return ed.Layout(gtx, st) })
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// commitRich saves the rich editor's cell when its text changed.
func (k *ctl) commitRich() {
	e := &k.edit
	ed, orig, rec, col := e.rich, e.richOrig, e.rec, e.col
	e.active, e.rich, e.richOrig = false, nil, nil
	if ed == nil || col == nil || rec == "" {
		return
	}
	doc := ed.Doc()
	plain := strings.TrimSpace(doc.Text())
	blocks := doc.RichTextBlocks()
	if canonRich(blocks) == canonRich(orig) {
		return // unchanged
	}
	edit := lists.Edit{RowID: rec, Column: col, Text: plain, Rich: blocks}
	if doc.Empty() {
		edit.Clear, edit.Rich = true, json.RawMessage("[]") // still the rich editor's write
	}
	k.save(edit)
}

// richRoundTrips reports whether the composer's editor reads raw and
// writes it back the same (canonRich), so editing it loses nothing.
func richRoundTrips(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	d, err := composer.DocFromBlocksJSON(raw, nil)
	if err != nil {
		return false
	}
	a, b := canonRich(raw), canonRich(d.RichTextBlocks())
	return a != "" && a == b
}

// rtNode is any rich_text block or element.
type rtNode struct {
	Type        string          `json:"type"`
	Elements    []rtNode        `json:"elements"`
	Style       json.RawMessage `json:"style"`
	Indent      int             `json:"indent"`
	Offset      int             `json:"offset"`
	Border      int             `json:"border"`
	Text        string          `json:"text"`
	URL         string          `json:"url"`
	UserID      string          `json:"user_id"`
	ChannelID   string          `json:"channel_id"`
	UsergroupID string          `json:"usergroup_id"`
	Range       string          `json:"range"`
	Name        string          `json:"name"`
	SkinTone    int             `json:"skin_tone"`
	Timestamp   json.Number     `json:"timestamp"`
	Format      string          `json:"format"`
	Fallback    string          `json:"fallback"`
	Unsafe      bool            `json:"unsafe"`
	ChannelTS   string          `json:"ts"`
}

// canonRich is what a reader sees in rich_text: its sections, lists
// (style, indent, offset, border), quotes and code blocks, and their runs
// by kind, text, target and style. Block ids, empty or false styles, a
// link's text equal to its URL, the trailing line break of a section and
// how text is split into elements don't count. "" when raw isn't
// rich_text Clack can read.
func canonRich(raw json.RawMessage) string {
	var blocks []rtNode
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, bl := range blocks {
		if bl.Type != "rich_text" {
			b.WriteString("[" + bl.Type + "]")
			continue
		}
		for _, sec := range bl.Elements {
			canonSection(&b, sec)
		}
	}
	return b.String()
}

func canonSection(b *strings.Builder, n rtNode) {
	switch n.Type {
	case "rich_text_list":
		b.WriteString("<list " + styleWord(n.Style) + " " + strconv.Itoa(n.Indent) + " " + strconv.Itoa(n.Offset) + " " + strconv.Itoa(n.Border) + ">")
		for _, it := range n.Elements {
			canonSection(b, it)
		}
		b.WriteString("</list>")
		return
	case "rich_text_section", "rich_text_quote", "rich_text_preformatted":
		b.WriteString("<" + n.Type + " " + strconv.Itoa(n.Border) + ">")
		canonInline(b, n.Elements)
		b.WriteString("</>")
		return
	}
	b.WriteString("<?" + n.Type + ">")
	canonInline(b, []rtNode{n})
}

// canonInline writes a run of inline elements, merging adjacent text of
// one style and dropping the section's final line break.
func canonInline(b *strings.Builder, els []rtNode) {
	type run struct{ key, text string }
	var runs []run
	for _, e := range els {
		st := styleFlags(e.Style)
		var r run
		switch e.Type {
		case "text":
			if e.Text == "" {
				continue
			}
			r = run{key: "t" + st, text: e.Text}
			if n := len(runs); n > 0 && runs[n-1].key == r.key {
				runs[n-1].text += e.Text
				continue
			}
		case "link":
			txt := e.Text
			if txt == e.URL {
				txt = ""
			}
			r = run{key: "link" + st + "|" + e.URL + "|" + txt}
		case "user":
			r = run{key: "user" + st + "|" + e.UserID}
		case "channel":
			r = run{key: "channel" + st + "|" + e.ChannelID}
		case "usergroup":
			r = run{key: "usergroup" + st + "|" + e.UsergroupID}
		case "broadcast":
			r = run{key: "broadcast" + st + "|" + e.Range}
		case "emoji":
			r = run{key: "emoji|" + e.Name + "|" + strconv.Itoa(e.SkinTone)}
		default: // dates and anything else: kept whole, so a writer that drops them differs
			bs, _ := json.Marshal(e)
			r = run{key: "?" + string(bs)}
		}
		runs = append(runs, r)
	}
	if n := len(runs); n > 0 && strings.HasPrefix(runs[n-1].key, "t") {
		runs[n-1].text = strings.TrimRight(runs[n-1].text, "\n")
		if runs[n-1].text == "" {
			runs = runs[:n-1]
		}
	}
	for _, r := range runs {
		b.WriteString("{" + r.key + ":" + r.text + "}")
	}
}

// styleWord is a list's style ("bullet", "ordered").
func styleWord(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// styleFlags is an inline style's true flags, sorted.
func styleFlags(raw json.RawMessage) string {
	var m map[string]bool
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return ""
	}
	var on []string
	for k, v := range m {
		if v {
			on = append(on, k)
		}
	}
	sort.Strings(on)
	if len(on) == 0 {
		return ""
	}
	return "(" + strings.Join(on, ",") + ")"
}
