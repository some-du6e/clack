// Package hunspell is a pure-Go spell checker for Hunspell dictionaries.
// It reads Chromium's binary dictionaries (.bdic, the files Slack's
// desktop app downloads from spellcheck.slack-edge.com/dictionaries_v2/
// and Chromium from its own CDN) and plain .aff/.dic pairs.
//
// It implements the parts of Hunspell's affix grammar the shipped
// dictionaries use: PFX/SFX rules with conditions and cross products,
// continuation classes (two suffixes, or a prefix and a suffix),
// FLAG long/num/UTF-8, AF aliases, NEEDAFFIX, FORBIDDENWORD, KEEPCASE,
// ONLYINCOMPOUND, NOSUGGEST, CIRCUMFIX, COMPOUNDRULE, COMPOUNDFLAG /
// COMPOUNDBEGIN / COMPOUNDMIDDLE / COMPOUNDEND with COMPOUNDMIN, ICONV,
// IGNORE, and suggestions from REP, MAP, KEY and TRY plus n-gram
// similarity over the word list, in Hunspell's order.
//
// This is a fresh implementation of the documented format (hunspell(5)),
// not a port of Hunspell's sources. A Dict is safe for concurrent Check
// and Suggest calls; Add must not run concurrently with them.
package hunspell

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type flag uint32

// flags is a small set of flags (kept sorted).
type flags []flag

func (fs flags) has(f flag) bool {
	if f == 0 {
		return false
	}
	for _, x := range fs {
		if x == f {
			return true
		}
	}
	return false
}

type flagMode uint8

const (
	flagChar flagMode = iota // one byte (Latin-1 in Hunspell; here one rune)
	flagLong                 // two characters
	flagNum                  // decimal numbers, comma separated
	flagUTF8                 // one rune
)

// affix is one PFX/SFX rule line.
type affix struct {
	flag   flag
	prefix bool
	cross  bool
	strip  string
	add    string
	cond   cond
	cont   flags // continuation classes
}

// cond is a rule condition: a sequence of character classes matched at
// the start (prefixes) or end (suffixes) of the stem.
type cond []condClass

type condClass struct {
	any   bool
	neg   bool
	runes []rune
}

func (c condClass) match(r rune) bool {
	if c.any {
		return true
	}
	in := false
	for _, x := range c.runes {
		if x == r {
			in = true
			break
		}
	}
	return in != c.neg
}

func parseCond(s string) cond {
	if s == "" || s == "." {
		return nil
	}
	var out cond
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '.':
			out = append(out, condClass{any: true})
		case '[':
			j := i + 1
			c := condClass{}
			if j < len(rs) && rs[j] == '^' {
				c.neg = true
				j++
			}
			for ; j < len(rs) && rs[j] != ']'; j++ {
				c.runes = append(c.runes, rs[j])
			}
			out = append(out, c)
			i = j
		default:
			out = append(out, condClass{runes: []rune{rs[i]}})
		}
	}
	return out
}

// matchEnd reports whether the condition matches the end of w.
func (c cond) matchEnd(w []rune) bool {
	if len(c) > len(w) {
		return false
	}
	off := len(w) - len(c)
	for i, cl := range c {
		if !cl.match(w[off+i]) {
			return false
		}
	}
	return true
}

// matchStart reports whether the condition matches the start of w.
func (c cond) matchStart(w []rune) bool {
	if len(c) > len(w) {
		return false
	}
	for i, cl := range c {
		if !cl.match(w[i]) {
			return false
		}
	}
	return true
}

// Dict is a loaded dictionary.
type Dict struct {
	words map[string][]flags // root → its homonyms' flag sets
	pfx   []*affix
	sfx   []*affix
	// bySfxEnd indexes suffixes by the last rune of their add string (0 for
	// empty adds), bySfxFlag and byPfxFlag by flag.
	bySfxEnd  map[rune][]*affix
	byPfxHead map[rune][]*affix
	bySfxFlag map[flag][]*affix
	byPfxFlag map[flag][]*affix

	mode    flagMode
	aliases []flags // AF lines (1-based in the file)

	needAffix, forbidden, keepCase, onlyInCompound, noSuggest, circumfix     flag
	compoundFlag, compoundBegin, compoundMiddle, compoundEnd, compoundPermit flag
	compoundMin                                                              int
	compoundRules                                                            [][]ruleAtom
	compoundRuleFlags                                                        map[flag]bool

	rep    [][2]string
	maps   [][]string // each MAP group: its members (runes or (multi) strings)
	key    []string   // KEY rows
	try    []rune
	iconv  [][2]string
	ignore []rune

	maxNgram int
	noSplit  bool // NOSPLITSUGS
}

type ruleAtom struct {
	f   flag
	rep byte // 0, '*' or '?'
}

func newDict() *Dict {
	return &Dict{
		words:       map[string][]flags{},
		bySfxEnd:    map[rune][]*affix{},
		byPfxHead:   map[rune][]*affix{},
		bySfxFlag:   map[flag][]*affix{},
		byPfxFlag:   map[flag][]*affix{},
		compoundMin: 3,
		maxNgram:    4,
	}
}

// parseFlags decodes a flag string in the dictionary's FLAG mode.
func (d *Dict) parseFlags(s string) flags {
	if s == "" {
		return nil
	}
	var out flags
	switch d.mode {
	case flagLong:
		rs := []rune(s)
		for i := 0; i+1 < len(rs); i += 2 {
			out = append(out, flag(rs[i])<<16|flag(rs[i+1]))
		}
	case flagNum:
		for _, p := range strings.Split(s, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n > 0 {
				out = append(out, flag(n))
			}
		}
	default:
		for _, r := range s {
			out = append(out, flag(r))
		}
	}
	return out
}

// flagsOf resolves a dic entry's flag field: an AF alias number when the
// dictionary has aliases, else literal flags.
func (d *Dict) flagsOf(s string) flags {
	if len(d.aliases) > 0 {
		if n, err := strconv.Atoi(s); err == nil {
			if n >= 1 && n <= len(d.aliases) {
				return d.aliases[n-1]
			}
			return nil
		}
	}
	return d.parseFlags(s)
}

func (d *Dict) oneFlag(s string) flag {
	fs := d.parseFlags(s)
	if len(fs) == 0 {
		return 0
	}
	return fs[0]
}

// parseAff reads .aff lines (the BDIC "other" and affix tables hold the
// same lines). Header lines of a PFX/SFX block ("SFX A Y 3") set the
// cross-product bit for the entries that follow.
func (d *Dict) parseAff(lines []string) {
	// FLAG must be known before anything that holds flags
	for _, ln := range lines {
		f := strings.Fields(ln)
		if len(f) >= 2 && f[0] == "FLAG" {
			switch f[1] {
			case "long":
				d.mode = flagLong
			case "num":
				d.mode = flagNum
			case "UTF-8":
				d.mode = flagUTF8
			}
		}
	}
	cross := map[string]bool{}
	afHeader := false
	for _, ln := range lines {
		if i := strings.IndexByte(ln, '#'); i == 0 {
			continue
		}
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "AF":
			if len(f) >= 2 {
				if !afHeader {
					afHeader = true // the first AF line is the count
					continue
				}
				d.aliases = append(d.aliases, d.parseFlags(f[1]))
			}
		case "PFX", "SFX":
			if len(f) == 4 && (f[2] == "Y" || f[2] == "N") {
				if _, err := strconv.Atoi(f[3]); err == nil {
					cross[f[0]+" "+f[1]] = f[2] == "Y"
					continue
				}
			}
			if len(f) < 4 {
				continue
			}
			a := &affix{flag: d.oneFlag(f[1]), prefix: f[0] == "PFX", cross: cross[f[0]+" "+f[1]]}
			if f[2] != "0" {
				a.strip = f[2]
			}
			add := f[3]
			if i := strings.IndexByte(add, '/'); i >= 0 {
				a.cont = d.flagsOf(add[i+1:])
				add = add[:i]
			}
			if add != "0" {
				a.add = d.ignored(add)
			}
			if len(f) >= 5 {
				a.cond = parseCond(f[4])
			}
			d.addAffix(a)
		case "REP":
			if len(f) == 3 {
				d.rep = append(d.rep, [2]string{strings.ReplaceAll(f[1], "_", " "), strings.ReplaceAll(f[2], "_", " ")})
			}
		case "MAP":
			if len(f) == 2 {
				if _, err := strconv.Atoi(f[1]); err == nil {
					continue
				}
				d.maps = append(d.maps, parseMap(f[1]))
			}
		case "KEY":
			if len(f) >= 2 {
				d.key = strings.Split(f[1], "|")
			}
		case "TRY":
			if len(f) >= 2 {
				d.try = []rune(f[1])
			}
		case "ICONV":
			if len(f) == 3 {
				d.iconv = append(d.iconv, [2]string{f[1], f[2]})
			}
		case "IGNORE":
			if len(f) >= 2 {
				d.ignore = []rune(f[1])
			}
		case "NEEDAFFIX", "PSEUDOROOT":
			d.needAffix = d.flagArg(f)
		case "FORBIDDENWORD":
			d.forbidden = d.flagArg(f)
		case "KEEPCASE":
			d.keepCase = d.flagArg(f)
		case "ONLYINCOMPOUND":
			d.onlyInCompound = d.flagArg(f)
		case "NOSUGGEST":
			d.noSuggest = d.flagArg(f)
		case "CIRCUMFIX":
			d.circumfix = d.flagArg(f)
		case "COMPOUNDFLAG":
			d.compoundFlag = d.flagArg(f)
		case "COMPOUNDBEGIN":
			d.compoundBegin = d.flagArg(f)
		case "COMPOUNDMIDDLE":
			d.compoundMiddle = d.flagArg(f)
		case "COMPOUNDEND", "COMPOUNDLAST":
			d.compoundEnd = d.flagArg(f)
		case "COMPOUNDPERMITFLAG":
			d.compoundPermit = d.flagArg(f)
		case "COMPOUNDMIN":
			if len(f) >= 2 {
				if n, err := strconv.Atoi(f[1]); err == nil {
					d.compoundMin = max(n, 1)
				}
			}
		case "COMPOUNDRULE":
			if len(f) == 2 {
				if _, err := strconv.Atoi(f[1]); err == nil {
					continue
				}
				d.addCompoundRule(f[1])
			}
		case "MAXNGRAMSUGS":
			if len(f) >= 2 {
				if n, err := strconv.Atoi(f[1]); err == nil {
					d.maxNgram = n
				}
			}
		case "NOSPLITSUGS":
			d.noSplit = true
		}
	}
	if d.forbidden == 0 {
		d.forbidden = flag(65510) // Hunspell's default FORBIDDENWORD
	}
}

func (d *Dict) flagArg(f []string) flag {
	if len(f) < 2 {
		return 0
	}
	return d.oneFlag(f[1])
}

func parseMap(s string) []string {
	var out []string
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '(' {
			j := i + 1
			for j < len(rs) && rs[j] != ')' {
				j++
			}
			out = append(out, string(rs[i+1:min(j, len(rs))]))
			i = j
			continue
		}
		out = append(out, string(rs[i]))
	}
	return out
}

func (d *Dict) addCompoundRule(s string) {
	var rule []ruleAtom
	if d.compoundRuleFlags == nil {
		d.compoundRuleFlags = map[flag]bool{}
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		var f flag
		switch {
		case rs[i] == '(':
			j := i + 1
			for j < len(rs) && rs[j] != ')' {
				j++
			}
			f = d.oneFlag(string(rs[i+1 : min(j, len(rs))]))
			i = j
		case rs[i] == '*' || rs[i] == '?':
			if len(rule) > 0 {
				rule[len(rule)-1].rep = byte(rs[i])
			}
			continue
		default:
			f = d.oneFlag(string(rs[i]))
		}
		rule = append(rule, ruleAtom{f: f})
		d.compoundRuleFlags[f] = true
	}
	d.compoundRules = append(d.compoundRules, rule)
}

func (d *Dict) addAffix(a *affix) {
	if a.prefix {
		d.pfx = append(d.pfx, a)
		var head rune
		if a.add != "" {
			head, _ = utf8.DecodeRuneInString(a.add)
		}
		d.byPfxHead[head] = append(d.byPfxHead[head], a)
		d.byPfxFlag[a.flag] = append(d.byPfxFlag[a.flag], a)
		return
	}
	d.sfx = append(d.sfx, a)
	var end rune
	if a.add != "" {
		end, _ = utf8.DecodeLastRuneInString(a.add)
	}
	d.bySfxEnd[end] = append(d.bySfxEnd[end], a)
	d.bySfxFlag[a.flag] = append(d.bySfxFlag[a.flag], a)
}

// addWord adds a root with its flags.
func (d *Dict) addWord(w string, fs flags) {
	w = d.ignored(w)
	if w == "" {
		return
	}
	d.words[w] = append(d.words[w], fs)
}

// ignored drops the IGNORE characters.
func (d *Dict) ignored(s string) string {
	if len(d.ignore) == 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		for _, x := range d.ignore {
			if x == r {
				return -1
			}
		}
		return r
	}, s)
}

// Add adds w as a correct word (a custom dictionary word). Not safe while
// Check or Suggest run.
func (d *Dict) Add(w string) {
	if w != "" {
		d.words[w] = append(d.words[w], nil)
	}
}

// Words is the number of roots.
func (d *Dict) Words() int { return len(d.words) }

// ParseAffDic loads a plain Hunspell .aff/.dic pair (UTF-8).
func ParseAffDic(aff, dic string) *Dict {
	d := newDict()
	d.parseAff(strings.Split(strings.ReplaceAll(aff, "\r\n", "\n"), "\n"))
	for i, ln := range strings.Split(strings.ReplaceAll(dic, "\r\n", "\n"), "\n") {
		ln = strings.TrimRight(ln, " \t")
		if ln == "" || (i == 0 && isDigits(ln)) {
			continue
		}
		if j := strings.IndexAny(ln, "\t"); j >= 0 {
			ln = ln[:j] // morphological fields
		}
		w, fl := ln, ""
		if j := slashIndex(ln); j >= 0 {
			w, fl = ln[:j], ln[j+1:]
		}
		if sp := strings.IndexByte(fl, ' '); sp >= 0 {
			fl = fl[:sp]
		}
		w = strings.ReplaceAll(w, `\/`, "/")
		d.addWord(w, d.flagsOf(fl))
	}
	return d
}

// slashIndex is the flag separator: the first '/' not escaped as "\/".
func slashIndex(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '/' && s[i-1] != '\\' {
			return i
		}
	}
	if len(s) > 0 && s[0] == '/' {
		return -1
	}
	return -1
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
