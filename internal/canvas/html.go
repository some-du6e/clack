package canvas

import (
	"html"
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// HTML writes a document in the shape of Quip's export (the vocabulary
// Parse reads: quip-canvas-content, data-section-style list and image
// wrappers, <control> mentions and emoji, <lnk> links, prettyprint code
// lines). The fake server serves it as a canvas's url_private; ids are the
// blocks' own (sec assigns one where a block has none).
func HTML(d Doc, sec func() string) string {
	if sec == nil {
		n := 0
		sec = func() string { n++; return "temp:C:GEN" + strconv.Itoa(n) }
	}
	w := &hw{sec: sec}
	w.s.WriteString(`<div class="quip-canvas-content">`)
	w.blocks(d.Blocks)
	w.s.WriteString(`</div>`)
	return w.s.String()
}

type hw struct {
	s   strings.Builder
	sec func() string
}

func (w *hw) id(b *Block) string {
	if b.ID == "" {
		b.ID = w.sec()
	}
	return html.EscapeString(b.ID)
}

var listStyle = map[Kind]string{Bullet: styleBullet, Numbered: styleNumbered, Check: styleCheck}

func (w *hw) blocks(bs []Block) {
	for i := 0; i < len(bs); i++ {
		b := &bs[i]
		if b.Kind.IsList() {
			j := i + 1
			for j < len(bs) && bs[j].Kind == b.Kind {
				j++
			}
			w.list(bs[i:j])
			i = j - 1
			continue
		}
		w.block(b)
	}
}

// list writes a run of same-kind items like Quip: one wrapper div whose
// --indent0 is the run's base level; deeper items in nested <ul>s.
func (w *hw) list(items []Block) {
	base := items[0].Level
	for _, it := range items {
		base = min(base, it.Level)
	}
	w.s.WriteString(`<div data-section-style="` + listStyle[items[0].Kind] + `" class="list-numbering-restart-at" style="--indent0: ` + strconv.Itoa(base) + `">`)
	w.s.WriteString(`<ul id="` + html.EscapeString(w.sec()) + `">`)
	depth := base
	prevNum := map[int]int{}
	for k := range items {
		it := &items[k]
		for depth < it.Level {
			w.s.WriteString("<ul>")
			depth++
		}
		for depth > it.Level {
			w.s.WriteString("</ul>")
			depth--
		}
		w.s.WriteString(`<li id="` + w.id(it) + `"`)
		if it.Kind == Check && it.Checked {
			w.s.WriteString(` class="checked"`)
		}
		if it.Kind == Numbered && it.Number > 0 && it.Number != prevNum[it.Level]+1 {
			w.s.WriteString(` value="` + strconv.Itoa(it.Number) + `"`)
		}
		prevNum[it.Level] = it.Number
		for l := range prevNum {
			if l > it.Level {
				delete(prevNum, l)
			}
		}
		w.s.WriteString(`><span id="` + html.EscapeString(w.sec()) + `">`)
		w.inline(it.Spans)
		w.s.WriteString(`</span><br/></li>`)
	}
	for depth > base {
		w.s.WriteString("</ul>")
		depth--
	}
	w.s.WriteString(`</ul></div>`)
}

func (w *hw) block(b *Block) {
	switch b.Kind {
	case Heading1, Heading2, Heading3:
		tag := map[Kind]string{Heading1: "h1", Heading2: "h2", Heading3: "h3"}[b.Kind]
		w.s.WriteString("<" + tag + ` id="` + w.id(b) + `">`)
		w.inline(b.Spans)
		w.s.WriteString("</" + tag + ">")
	case Code:
		id := w.id(b)
		for i, l := range strings.Split(b.Code, "\n") {
			lid := id
			if i > 0 {
				lid = html.EscapeString(w.sec())
			}
			w.s.WriteString(`<p id="` + lid + `" class="prettyprint line">` + html.EscapeString(l) + `</p>`)
		}
	case Divider:
		w.id(b)
		w.s.WriteString(`<hr style='width:100%'>`)
	case Quote:
		w.id(b)
		w.s.WriteString("<blockquote>")
		if len(b.Children) == 0 {
			w.s.WriteString(`<p id="` + html.EscapeString(w.sec()) + `" class="line">`)
			w.inline(b.Spans)
			w.s.WriteString("</p>")
		}
		w.blocks(b.Children)
		w.s.WriteString("</blockquote>")
	case Callout:
		w.s.WriteString(`<div id="` + w.id(b) + `" class="callout" data-emoji="` + html.EscapeString(b.Emoji) + `">`)
		w.blocks(b.Children)
		w.s.WriteString("</div>")
	case Table:
		w.id(b)
		h := "false"
		if b.Header {
			h = "true"
		}
		w.s.WriteString(`<table data-header="` + h + `">`)
		for r, row := range b.Rows {
			w.s.WriteString("<tr>")
			tag := "td"
			if r == 0 && b.Header {
				tag = "th"
			}
			for _, c := range row {
				w.s.WriteString("<" + tag + ">")
				w.blocks(c.Blocks)
				w.s.WriteString("</" + tag + ">")
			}
			w.s.WriteString("</tr>")
		}
		w.s.WriteString("</table>")
	case Image:
		if b.Image == nil {
			return
		}
		src := b.Image.URL
		if b.FileID != "" && src == "" {
			src = "/collab-slack-blob/FAKE/" + b.FileID + "?size_name=original"
		}
		w.s.WriteString(`<div data-section-style="11" class=''><img src='` + html.EscapeString(src) + `' id='` + w.id(b) +
			`' alt="` + html.EscapeString(b.Image.Alt) + `" width='` + strconv.Itoa(b.Image.W) + `' height='` + strconv.Itoa(b.Image.H) + `'></img></div>`)
	case Embed:
		w.s.WriteString(`<p id="` + w.id(b) + `" class='embedded-link'>` + html.EscapeString(firstNonEmpty(b.Title, b.URL)) + `</p>`)
	case File:
		w.s.WriteString(`<p id="` + w.id(b) + `" class='embedded-file'>`)
		if b.FileID != "" {
			w.s.WriteString(`<lnk data-slack-file-id="sf:` + html.EscapeString(b.FileID) + `">` + html.EscapeString(b.Title) + `</lnk>`)
		} else {
			w.s.WriteString(html.EscapeString(b.Title))
		}
		w.s.WriteString(`</p>`)
	case Columns:
		w.s.WriteString(`<div class='flexbox'>`)
		for _, col := range b.Columns {
			w.s.WriteString(`<div class='flexbox-column'>`)
			w.blocks(col)
			w.s.WriteString(`</div>`)
		}
		w.s.WriteString(`</div>`)
	default:
		w.s.WriteString(`<p id="` + w.id(b) + `" class="line">`)
		w.inline(b.Spans)
		w.s.WriteString(`</p>`)
	}
}

func (w *hw) inline(spans []richtext.Span) {
	for _, s := range spans {
		var open, close string
		for _, t := range []struct {
			f   richtext.Style
			tag string
		}{{richtext.Bold, "b"}, {richtext.Italic, "i"}, {richtext.Strike, "del"}, {richtext.Underline, "u"}, {richtext.Code, "code"}} {
			if s.Style&t.f != 0 {
				open += "<" + t.tag + ">"
				close = "</" + t.tag + ">" + close
			}
		}
		if s.Style&richtext.Highlight != 0 {
			open += `<span style="background-color:#fae7b4">`
			close = "</span>" + close
		}
		w.s.WriteString(open)
		switch s.Kind {
		case richtext.Break:
			w.s.WriteString("<br>")
		case richtext.Link:
			w.s.WriteString(`<lnk href="` + html.EscapeString(s.URL) + `">` + html.EscapeString(s.Text) + `</lnk>`)
		case richtext.User, richtext.Channel, richtext.Broadcast, richtext.Usergroup:
			w.s.WriteString(`<control id="` + html.EscapeString(w.sec()) + `" data-remapped="true"><a>` + html.EscapeString(s.Text) + `</a></control>`)
		case richtext.Date:
			w.s.WriteString(`<control id="` + html.EscapeString(w.sec()) + `" data-remapped="true">` + html.EscapeString(s.Text) + `</control>`)
		case richtext.Emoji:
			src := s.URL
			if src == "" {
				src = "https://slack-imgs.com/?c=1&o1=gu&url=emoji/" + s.ID
			}
			w.s.WriteString(`<control id="` + html.EscapeString(w.sec()) + `" data-remapped="true"><img src="` + html.EscapeString(src) +
				`" alt="` + html.EscapeString(s.ID) + `" data-is-slack style="width: 18px; height: 18px"></control>`)
		case richtext.InlineImage:
			w.s.WriteString(`<img src="` + html.EscapeString(s.URL) + `" alt="` + html.EscapeString(s.Text) + `">`)
		default:
			w.s.WriteString(html.EscapeString(s.Text))
		}
		w.s.WriteString(close)
	}
}
