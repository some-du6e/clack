package composer

import (
	"image"
	"image/color"
	"strings"
	"unicode"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Syntax colours in the snippet editor, the way Slack's CodeMirror does
// them (lYxy getCodeMirrorOptions): only once a Type with a CodeMirror
// mode is picked (its loader ran: Auto Detect Type and Plain Text have
// none, so they stay plain), in the client theme's CodeMirror theme
// (FkeY: "default" in light mode, "yonce" in dark), whose token classes
// colour comments, strings, numbers, atoms, keywords, definitions and
// variables. Clack has no CodeMirror modes; it reads each line with the
// Code tab's tokenizer (core.HighlightLine) and the modes' keyword lists
// for the common languages, and paints the tokens in those classes'
// colours over the editor's own text layout (Regions), so carets and
// selections stay the editor's.

// cmClass is a CodeMirror token class.
type cmClass uint8

const (
	cmPlain cmClass = iota
	cmComment
	cmString
	cmString2 // escapes, regexes
	cmNumber
	cmAtom // true, false, null, …
	cmKeyword
	cmDef
)

// cmColors are the theme's .cm-<class> colours: cm-s-default (light) and
// cm-s-yonce (dark), from the stylesheet Slack ships with CodeMirror.
func cmColors(dark bool) [8]color.NRGBA {
	if dark {
		return [8]color.NRGBA{
			cmPlain: rgbHex(0xd1d2d3), cmComment: rgbHex(0x696d70), cmString: rgbHex(0xe6db74), cmString2: rgbHex(0xf39b35),
			cmNumber: rgbHex(0xa06fca), cmAtom: rgbHex(0xf39b35), cmKeyword: rgbHex(0x00a7aa), cmDef: rgbHex(0x98e342),
		}
	}
	return [8]color.NRGBA{
		cmPlain: rgbHex(0x000000), cmComment: rgbHex(0xaa5500), cmString: rgbHex(0xaa1111), cmString2: rgbHex(0xff5500),
		cmNumber: rgbHex(0x116644), cmAtom: rgbHex(0x221199), cmKeyword: rgbHex(0x770088), cmDef: rgbHex(0x0000ff),
	}
}

// snippetLangs maps the snippet types Clack highlights to the Code tab
// tokenizer's languages; other types draw plain.
var snippetLangs = map[string]string{
	"c": "c", "cpp": "cpp", "csharp": "csharp", "java": "java", "javascript": "javascript", "typescript": "typescript",
	"go": "go", "rust": "rust", "swift": "swift", "kotlin": "kotlin", "scala": "scala", "objc": "objective-c", "dart": "dart",
	"php": "php", "groovy": "groovy", "python": "python", "ruby": "ruby", "shell": "shellscript", "powershell": "shellscript",
	"yaml": "yaml", "toml": "toml", "sql": "sql", "lua": "lua", "haskell": "haskell", "elm": "elm", "erlang": "erlang",
	"clojure": "clojure", "html": "html", "xml": "xml", "markdown": "markdown", "css": "css", "less": "less", "scss": "scss",
	"json": "json", "r": "r", "elixir": "elixir", "dockerfile": "dockerfile", "protobuf": "protobuf",
}

func kw(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// The modes' keyword lists (clike, javascript, python, ruby, shell, sql,
// go, rust, lua).
var (
	kwC      = kw("auto break case char const continue default do double else enum extern float for goto if inline int long register restrict return short signed sizeof static struct switch typedef union unsigned void volatile while bool")
	kwCpp    = kw("class namespace new delete public private protected virtual template typename this throw try catch using operator friend explicit mutable constexpr nullptr")
	kwJava   = kw("abstract assert boolean byte catch class extends final finally implements import instanceof interface native new package private protected public super synchronized this throw throws transient try var")
	kwJS     = kw("break case catch class const continue debugger default delete do else export extends finally for function if import in instanceof let new return super switch this throw try typeof var void while with yield async await of static get set from as")
	kwGo     = kw("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var")
	kwRust   = kw("as break const continue crate else enum extern fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait type unsafe use where while async await dyn")
	kwPython = kw("and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield print exec")
	kwRuby   = kw("alias and BEGIN begin break case class def defined? do else elsif END end ensure for if in module next not or redo rescue retry return self super then undef unless until when while yield")
	kwShell  = kw("if then else elif fi for while until do done case esac function in select return exit export local readonly declare unset shift break continue")
	kwSQL    = kw("select from where insert into values update set delete create table drop alter index join left right inner outer full cross on and or not null as group by order having limit offset union all distinct case when then else end exists in between like is primary key foreign references default view")
	kwLua    = kw("and break do else elseif end for function goto if in local not or repeat return then until while")
)

// snippetKeywords is the type's mode's keywords (nil: none).
func snippetKeywords(lang string) (map[string]bool, bool) {
	switch lang {
	case "c":
		return kwC, false
	case "cpp", "objective-c":
		return union(kwC, kwCpp), false
	case "csharp", "java", "kotlin", "scala", "groovy", "dart":
		return union(kwC, kwJava), false
	case "javascript", "typescript", "php", "swift":
		return kwJS, false
	case "go":
		return kwGo, false
	case "rust":
		return kwRust, false
	case "python":
		return kwPython, false
	case "ruby", "elixir":
		return kwRuby, false
	case "shellscript", "dockerfile":
		return kwShell, false
	case "sql":
		return kwSQL, true // case-insensitive
	case "lua":
		return kwLua, false
	}
	return nil, false
}

func union(a, b map[string]bool) map[string]bool {
	m := make(map[string]bool, len(a)+len(b))
	for k := range a {
		m[k] = true
	}
	for k := range b {
		m[k] = true
	}
	return m
}

// cmSpan is a run of one class on a line, in runes from the line start.
type cmSpan struct {
	start, end int
	class      cmClass
}

// snippetMaxHighlight is CodeMirror's maxHighlightLength: longer lines
// draw plain.
const snippetMaxHighlight = 10000

// highlightLine classes line for lang.
func highlightLine(lang, line string) []cmSpan {
	if line == "" || utf8.RuneCountInString(line) > snippetMaxHighlight {
		return nil
	}
	toks := core.HighlightLine(lang, line)
	if toks == nil {
		return nil
	}
	kws, fold := snippetKeywords(lang)
	var out []cmSpan
	at := 0
	for _, t := range toks {
		n := utf8.RuneCountInString(t.Text)
		switch t.Kind {
		case core.TokComment:
			out = append(out, cmSpan{at, at + n, cmComment})
		case core.TokString:
			out = append(out, cmSpan{at, at + n, cmString})
		case core.TokEscape:
			out = append(out, cmSpan{at, at + n, cmString2})
		case core.TokConstant:
			c := cmAtom
			if r, _ := utf8.DecodeRuneInString(t.Text); unicode.IsDigit(r) || r == '.' || r == '-' {
				c = cmNumber
			}
			out = append(out, cmSpan{at, at + n, c})
		case core.TokEntity:
			out = append(out, cmSpan{at, at + n, cmDef})
		case core.TokPlain:
			if kws == nil {
				break
			}
			// the mode's keywords inside plain text
			rs := []rune(t.Text)
			for i := 0; i < len(rs); {
				if !isIdentRune(rs[i]) {
					i++
					continue
				}
				j := i
				for j < len(rs) && (isIdentRune(rs[j]) || rs[j] == '?' && lang == "ruby") {
					j++
				}
				w := string(rs[i:j])
				if fold {
					w = strings.ToLower(w)
				}
				if kws[w] {
					out = append(out, cmSpan{at + i, at + j, cmKeyword})
				}
				i = j
			}
		}
		at += n
	}
	return out
}

func isIdentRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// paintHighlights draws the classed runs of the editor's text over its
// layout (the editor drew its text clear), plain runs in the plain
// colour. gtx is the editor's frame.
func (s *snippetCode) paintHighlights(gtx layout.Context, th *theme.Theme, lang string, mono font.Font) {
	cols := cmColors(th.Dark)
	txt := s.ed.Text()
	st := textStyle{Size: snippetCodeSize, LineHeight: snippetCodeLine, Typeface: mono.Typeface, Color: cols[cmPlain]}
	pos := 0 // rune index of the line's start in the whole text
	lines := strings.Split(txt, "\n")
	if len(lines) > 2000 {
		lines = lines[:2000]
	}
	for _, ln := range lines {
		n := utf8.RuneCountInString(ln)
		spans := highlightLine(lang, ln)
		rs := []rune(ln)
		at := 0
		draw := func(a, b int, c cmClass) {
			st.Color = cols[c]
			for a < b { // tabs are the editor's (tab stops): pieces between them
				for a < b && rs[a] == '\t' {
					a++
				}
				e := a
				for e < b && rs[e] != '\t' {
					e++
				}
				if e > a {
					s.paintRun(gtx, th, st, string(rs[a:e]), pos+a, pos+e)
				}
				a = e
			}
		}
		for _, sp := range spans {
			draw(at, sp.start, cmPlain)
			draw(sp.start, sp.end, sp.class)
			at = sp.end
		}
		draw(at, n, cmPlain)
		pos += n + 1
	}
}

// paintRun draws s (runes [start, end) of the text) where the editor laid
// it out, one segment per visual line it spans.
func (s *snippetCode) paintRun(gtx layout.Context, th *theme.Theme, st textStyle, txt string, start, end int) {
	s.regions = s.ed.Regions(start, end, s.regions[:0])
	if len(s.regions) == 1 {
		s.paintAt(gtx, th, st, txt, s.regions[0])
		return
	}
	// wrapped (or tabbed into pieces): rune by rune
	rs := []rune(txt)
	for i := range rs {
		s.regions = s.ed.Regions(start+i, start+i+1, s.regions[:0])
		if len(s.regions) > 0 {
			s.paintAt(gtx, th, st, string(rs[i]), s.regions[0])
		}
	}
}

func (s *snippetCode) paintAt(gtx layout.Context, th *theme.Theme, st textStyle, txt string, r widget.Region) {
	if strings.TrimSpace(txt) == "" {
		return
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	dd := label(g, th.Shaper, st, txt)
	call := m.Stop()
	base := r.Bounds.Max.Y - r.Baseline
	off := op.Offset(image.Pt(r.Bounds.Min.X, base-(dd.Size.Y-dd.Baseline))).Push(gtx.Ops)
	call.Add(gtx.Ops)
	off.Pop()
}
