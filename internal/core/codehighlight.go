package core

// Syntax colours for the Code tab's patch lines. Slack highlights each
// line on its own (useHighlighter: shiki codeToTokensBase per line, the
// language from the file extension) with its "slalabaster" theme (module
// OH3S), which colours only these TextMate scopes:
//
//	comment                              → plt tomato 70
//	string, string.regexp, constant.other.symbol → plt cilantro 70
//	constant.character.escape            → content ter
//	constant.numeric/character/keyword, constant → plt indigo 70
//	entity.name                          → plt aubergine 70
//	punctuation                          → content ter
//	everything else (keywords, operators, variables) → content pry
//
// Clack has no TextMate engine; this tokenizer recognises those scopes
// for the languages in the extension map, line by line like Slack.

import "strings"

// Token kinds (the theme's scopes).
const (
	TokPlain = iota
	TokComment
	TokString
	TokEscape
	TokConstant // numbers, true/false/null, …
	TokEntity   // function, type and tag names
	TokPunct
)

// CodeToken is a run of one kind.
type CodeToken struct {
	Kind int
	Text string
}

type langSyntax struct {
	line       []string // line comment openers
	block      [2]string
	quotes     string
	constants  map[string]bool
	declarers  map[string]bool // the next identifier is an entity.name
	markup     bool            // html/xml/vue/markdown tags
	callEntity bool            // ident( is entity.name.function
}

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

var (
	cConsts   = words("true false null nil undefined NaN Infinity None True False this self super iota")
	cDeclares = words("function func fn def class struct interface type enum trait impl module object record package")
)

func syntaxOf(lang string) *langSyntax {
	cl := &langSyntax{line: []string{"//"}, block: [2]string{"/*", "*/"}, quotes: `"'` + "`", constants: cConsts, declarers: cDeclares, callEntity: true}
	switch lang {
	case "":
		return nil
	case "python", "ruby", "shellscript", "yaml", "toml", "r", "elixir", "dockerfile", "graphql", "protobuf":
		s := *cl
		s.line, s.block = []string{"#"}, [2]string{}
		if lang == "protobuf" {
			s.line = []string{"//"}
		}
		if lang == "yaml" || lang == "toml" || lang == "dockerfile" {
			s.callEntity = false
		}
		return &s
	case "sql", "lua", "haskell", "elm":
		s := *cl
		s.line, s.block = []string{"--"}, [2]string{}
		return &s
	case "erlang":
		s := *cl
		s.line, s.block = []string{"%"}, [2]string{}
		return &s
	case "clojure":
		s := *cl
		s.line, s.block, s.quotes = []string{";"}, [2]string{}, `"`
		return &s
	case "html", "xml", "vue", "markdown":
		return &langSyntax{block: [2]string{"<!--", "-->"}, quotes: `"'`, constants: map[string]bool{}, markup: true}
	case "css", "less", "scss":
		s := *cl
		if lang == "css" {
			s.line = nil
		}
		s.constants, s.declarers, s.callEntity = map[string]bool{}, map[string]bool{}, false
		return &s
	case "json":
		return &langSyntax{quotes: `"`, constants: words("true false null")}
	}
	return cl
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdent(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }

// HighlightLine splits one line of lang (CodeLang) into tokens; nil when
// the language isn't highlighted, so the caller draws plain text.
func HighlightLine(lang, line string) []CodeToken {
	syn := syntaxOf(lang)
	if syn == nil {
		return nil
	}
	if len(line) > 1000 { // shiki's tokenizeMaxLineLength
		return []CodeToken{{TokPlain, line}}
	}
	var out []CodeToken
	push := func(kind int, s string) {
		if s == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Kind == kind {
			out[n-1].Text += s
			return
		}
		out = append(out, CodeToken{kind, s})
	}
	declare := false
	inTag := false
	for i := 0; i < len(line); {
		rest := line[i:]
		if syn.block[0] != "" && strings.HasPrefix(rest, syn.block[0]) {
			end := strings.Index(rest[len(syn.block[0]):], syn.block[1])
			n := len(rest)
			if end >= 0 {
				n = len(syn.block[0]) + end + len(syn.block[1])
			}
			push(TokComment, rest[:n])
			i += n
			continue
		}
		if syn.block[1] != "" && i == 0 && strings.Contains(rest, syn.block[1]) && !strings.Contains(rest, syn.block[0]) &&
			(strings.HasPrefix(strings.TrimSpace(rest), "*") || strings.HasPrefix(strings.TrimSpace(rest), syn.block[1])) {
			n := strings.Index(rest, syn.block[1]) + len(syn.block[1]) // the tail of a block comment
			push(TokComment, rest[:n])
			i += n
			continue
		}
		lineComment := false
		for _, lc := range syn.line {
			if strings.HasPrefix(rest, lc) {
				lineComment = true
			}
		}
		if lineComment {
			push(TokComment, rest)
			break
		}
		c := line[i]
		if strings.IndexByte(syn.quotes, c) >= 0 && (!syn.markup || inTag) {
			j := i + 1
			push(TokString, string(c))
			start := j
			for j < len(line) && line[j] != c {
				if line[j] == '\\' && j+1 < len(line) && !syn.markup {
					push(TokString, line[start:j])
					push(TokEscape, line[j:j+2])
					j += 2
					start = j
					continue
				}
				j++
			}
			push(TokString, line[start:min(j, len(line))])
			if j < len(line) {
				push(TokString, string(c))
				j++
			}
			i = j
			continue
		}
		if syn.markup {
			if c == '<' {
				j := i + 1
				if j < len(line) && (line[j] == '/' || line[j] == '!' || line[j] == '?') {
					j++
				}
				push(TokPunct, line[i:j])
				k := j
				for k < len(line) && (isIdent(line[k]) || line[k] == '-' || line[k] == ':') {
					k++
				}
				push(TokEntity, line[j:k])
				inTag = k > j
				i = k
				continue
			}
			if inTag && (c == '>' || (c == '/' && i+1 < len(line) && line[i+1] == '>')) {
				n := 1
				if c == '/' {
					n = 2
				}
				push(TokPunct, line[i:i+n])
				inTag = false
				i += n
				continue
			}
			if inTag && c == '=' {
				push(TokPunct, "=")
				i++
				continue
			}
			push(TokPlain, string(c))
			i++
			continue
		}
		if c >= '0' && c <= '9' && (i == 0 || !isIdent(line[i-1])) {
			j := i + 1
			for j < len(line) && (isIdent(line[j]) || line[j] == '.') {
				j++
			}
			push(TokConstant, line[i:j])
			i = j
			continue
		}
		if isIdentStart(c) {
			j := i + 1
			for j < len(line) && isIdent(line[j]) {
				j++
			}
			w := line[i:j]
			k := j
			for k < len(line) && line[k] == ' ' {
				k++
			}
			switch {
			case syn.constants[w]:
				push(TokConstant, w)
			case declare:
				push(TokEntity, w)
				declare = false
			case syn.declarers[w]:
				push(TokPlain, w)
				declare = true
				i = j
				continue
			case syn.callEntity && j < len(line) && line[j] == '(':
				push(TokEntity, w)
			default:
				push(TokPlain, w)
			}
			declare = false
			i = j
			continue
		}
		if strings.IndexByte("()[]{};,.:", c) >= 0 {
			push(TokPunct, string(c))
			if c != '.' {
				declare = false
			}
			i++
			continue
		}
		push(TokPlain, string(c))
		i++
	}
	return out
}
