package composer

import (
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// Smart quotes and symbols, Preferences > Advanced > "Enable smart quotes
// and symbols" (users.prefs disable_smart_symbols, behind the
// composer_smart_symbols experiment). Slack's texty TextSubstitutionsModule
// (modern.vendor, build a84662d2d135):
//
//   - Space after "--" (not "---") turns it into "— " (onSmartSubstitution,
//     /(^|[^-])--$/ → "— ").
//   - Space after "->", "<-" or "<->" (not after another "-") turns it into
//     the arrow and a space (rD: → ← ↔).
//   - "." typed after ".." (not "...") makes "…" (onEllipsis).
//   - A typed ' or " becomes a curly quote (onQuoteKeydown with
//     getTypeTimeSmartQuote): nothing after a digit, nor ' after ":"; an
//     opening quote at the start or after whitespace or ([{<“‘, a closing
//     one otherwise.
//
// None of them apply inside code, a code block or a link (the bindings'
// format {code:false, "code-block":false}; the quote handler also skips
// links), and each is its own undo step: undo brings back what was typed
// (applySubstitution rewrites the history stack the same way).

// smartSymbolsOn is getSmartSymbolsEnabled: the experiment on and the
// pref not set. Tests may swap it.
var smartSymbolsOn = func(a *core.App) bool { return a != nil && coreAPI{a}.smartSymbols() }

// SmartSymbols reports whether the composer makes smart quotes and symbols.
func SmartSymbols(a *core.App) bool { return smartSymbolsOn(a) }

var smartArrows = map[string]string{"->": "→", "<-": "←", "<->": "↔"}

// smartSubstitute runs after the rune at caret-1 was typed. It returns the
// replaced range [from, caret) and its new text, or ok=false.
func smartSubstitute(d *Doc, caret int, plain bool) (from int, repl string, ok bool) {
	k := caret - 1
	if k < 0 || k >= d.Len() || d.Attrs[k].Atom != nil {
		return 0, "", false
	}
	if smartInCode(d, k, plain) {
		return 0, "", false
	}
	before := func(n int) string { // up to n runes before k on k's line
		ls := d.LineStart(d.LineOf(k))
		i := max(ls, k-n)
		for j := i; j < k; j++ {
			if d.Attrs[j].Atom != nil {
				i = j + 1
			}
		}
		return string(d.Runes[i:k])
	}
	switch r := d.Runes[k]; r {
	case ' ':
		b := before(4)
		if strings.HasSuffix(b, "--") && !strings.HasSuffix(b, "---") {
			return k - 2, "— ", true
		}
		for _, a := range []string{"<->", "->", "<-"} {
			if strings.HasSuffix(b, a) && !strings.HasSuffix(b, "-"+a) {
				return k - len([]rune(a)), smartArrows[a] + " ", true
			}
		}
	case '.':
		b := before(3)
		if strings.HasSuffix(b, "..") && !strings.HasSuffix(b, "...") {
			return k - 2, "…", true
		}
	case '"', '\'':
		if d.Attrs[k].Link != "" {
			return 0, "", false
		}
		prev := ""
		if k > d.LineStart(d.LineOf(k)) {
			if d.Attrs[k-1].Atom != nil {
				prev = "a"
			} else {
				prev = string(d.Runes[k-1])
			}
		} else if k > 0 {
			prev = "\n"
		}
		if q := typeTimeSmartQuote(r, prev); q != "" {
			return k, q, true
		}
	}
	return 0, "", false
}

// typeTimeSmartQuote is getTypeTimeSmartQuote(quote, charBefore).
func typeTimeSmartQuote(q rune, prev string) string {
	if prev != "" && prev[0] >= '0' && prev[0] <= '9' || q == '\'' && prev == ":" {
		return ""
	}
	open := prev == "" || strings.ContainsAny(prev, " \t\n ([{<“‘")
	switch {
	case q == '"' && open:
		return "“"
	case q == '"':
		return "”"
	case open:
		return "‘"
	}
	return "’"
}

// smartInCode: the rune at k is in inline code or a code block (or, with
// markup kept as typed, after an unclosed ` on its line or inside ```).
func smartInCode(d *Doc, k int, plain bool) bool {
	if d.Attrs[k].Style&Code != 0 || d.Lines[d.LineOf(k)].Kind == CodeBlock {
		return true
	}
	if !plain {
		return false
	}
	text := string(d.Runes[:k])
	if strings.Count(text, "```")%2 == 1 {
		return true
	}
	line := text[strings.LastIndexByte(text, '\n')+1:]
	return strings.Count(strings.ReplaceAll(line, "```", ""), "`")%2 == 1
}

// smartSymbols applies a substitution after a typed rune, keeping the
// literal text as its own undo step. It reports whether it changed d.
func (e *Editor) smartSymbols() bool {
	if !e.smart || e.composing() {
		return false
	}
	from, repl, ok := smartSubstitute(e.doc, e.caret, e.plainText)
	if !ok {
		return false
	}
	literal := e.snap()
	a := e.doc.Attrs[e.caret-1]
	a.Atom = nil
	e.doc.Delete(from, e.caret)
	end := e.doc.Insert(from, repl, a)
	e.anchor, e.caret = end, end
	e.pushUndo(literal)
	e.redo = e.redo[:0]
	e.lastKind = edOther
	e.commit()
	return true
}
