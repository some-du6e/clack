package hunspell

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// capType is Hunspell's casing class of a word.
type capType uint8

const (
	capNone  capType = iota // lower case
	capInit                 // Initial capital
	capAll                  // ALL CAPS
	capMixed                // mIxEd (or McDonald)
)

func caseOf(w string) capType {
	upper, letters := 0, 0
	first := false
	for i, r := range w {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) || unicode.IsTitle(r) {
				upper++
				if i == 0 {
					first = true
				}
			}
		}
	}
	switch {
	case upper == 0:
		return capNone
	case upper == letters:
		return capAll
	case upper == 1 && first:
		return capInit
	}
	return capMixed
}

func initCap(w string) string {
	r, n := utf8.DecodeRuneInString(w)
	if n == 0 {
		return w
	}
	return string(unicode.ToTitle(r)) + strings.ToLower(w[n:])
}

func lowerFirst(w string) string {
	r, n := utf8.DecodeRuneInString(w)
	if n == 0 {
		return w
	}
	return string(unicode.ToLower(r)) + w[n:]
}

// Check reports whether word is spelled correctly.
func (d *Dict) Check(word string) bool {
	word = d.convert(strings.TrimSpace(word))
	if word == "" {
		return true
	}
	if isNumber(word) {
		return true
	}
	ok, forbidden := d.checkCased(word)
	if ok || forbidden {
		return ok
	}
	// trailing dots: abbreviations ("etc.")
	if t := strings.TrimRight(word, "."); t != word && t != "" {
		ok, _ = d.checkCased(t)
		return ok
	}
	return false
}

// convert applies ICONV and IGNORE.
func (d *Dict) convert(w string) string {
	for _, c := range d.iconv {
		w = strings.ReplaceAll(w, c[0], c[1])
	}
	return d.ignored(w)
}

// isNumber is Hunspell's number rule: digits with single '.', ',' or '-'
// separators between them.
func isNumber(w string) bool {
	digit, sep := false, false
	for i, r := range w {
		switch {
		case r >= '0' && r <= '9':
			digit, sep = true, false
		case (r == '.' || r == ',' || r == '-') && digit && !sep && i < len(w)-1:
			sep = true
		case r == '-' && i == 0:
		default:
			return false
		}
	}
	return digit && !sep
}

// checkCased checks w in the casings Hunspell tries for its case class.
func (d *Dict) checkCased(w string) (ok, forbidden bool) {
	switch caseOf(w) {
	case capNone, capMixed:
		return d.lookupWord(w, false)
	case capInit:
		if ok, f := d.lookupWord(w, false); ok || f {
			return ok, f
		}
		lw := strings.ToLower(w)
		return d.lookupWord(lw, true)
	case capAll:
		if ok, f := d.lookupWord(w, false); ok || f {
			return ok, f
		}
		if ok, f := d.lookupWord(strings.ToLower(w), true); ok || f {
			return ok, f
		}
		return d.lookupWord(initCap(w), true)
	}
	return false, false
}

// lookupWord checks one casing: as a root, with affixes, or as a compound.
// recased says w is a case-folded form of what was typed (KEEPCASE words
// don't accept that).
func (d *Dict) lookupWord(w string, recased bool) (ok, forbidden bool) {
	if hs, found := d.words[w]; found {
		for _, fs := range hs {
			if fs.has(d.forbidden) {
				return false, true
			}
		}
		for _, fs := range hs {
			if d.rootOK(fs, recased) {
				return true, false
			}
		}
	}
	if d.affixed(w, recased) {
		return true, false
	}
	if d.compound(w) {
		return true, false
	}
	return false, false
}

func (d *Dict) rootOK(fs flags, recased bool) bool {
	if fs.has(d.needAffix) || fs.has(d.onlyInCompound) {
		return false
	}
	if recased && fs.has(d.keepCase) {
		return false
	}
	return true
}

// homonymHas reports whether root has a homonym with flag f (and, if g
// is set, also g) that may take affixes.
func (d *Dict) homonymHas(root string, f, g flag, recased bool) bool {
	for _, fs := range d.words[root] {
		if fs.has(d.forbidden) || (recased && fs.has(d.keepCase)) {
			continue
		}
		if fs.has(f) && (g == 0 || fs.has(g)) {
			return true
		}
	}
	return false
}

// affixed checks w = [prefix] root [suffix [suffix]].
func (d *Dict) affixed(w string, recased bool) bool {
	rw := []rune(w)
	// suffixes, with an optional second (outer) suffix and cross products
	if d.suffixCheck(rw, nil, recased, 0) {
		return true
	}
	// prefixes
	for _, p := range d.prefixesFor(rw) {
		stem := p.stem(rw)
		if stem == nil {
			continue
		}
		s := string(stem)
		if !p.cont.has(d.needAffix) && !p.cont.has(d.circumfix) && d.homonymHas(s, p.flag, 0, recased) {
			return true
		}
		// prefix + suffix (both cross product), or a suffix the prefix
		// allows through its continuation classes
		if d.suffixCheck(stem, p, recased, 0) {
			return true
		}
	}
	return false
}

// prefixesFor returns the prefixes whose add string starts w.
func (d *Dict) prefixesFor(w []rune) []*affix {
	var out []*affix
	out = append(out, d.byPfxHead[0]...)
	if len(w) > 0 {
		for _, p := range d.byPfxHead[w[0]] {
			if strings.HasPrefix(string(w), p.add) {
				out = append(out, p)
			}
		}
	}
	return out
}

// suffixesFor returns the suffixes whose add string ends w.
func (d *Dict) suffixesFor(w []rune) []*affix {
	var out []*affix
	out = append(out, d.bySfxEnd[0]...)
	if len(w) > 0 {
		for _, s := range d.bySfxEnd[w[len(w)-1]] {
			if strings.HasSuffix(string(w), s.add) {
				out = append(out, s)
			}
		}
	}
	return out
}

// stem undoes the affix on w, or nil when its condition fails.
func (a *affix) stem(w []rune) []rune {
	add := utf8.RuneCountInString(a.add)
	if add > len(w) {
		return nil
	}
	if a.prefix {
		if add == len(w) && a.strip == "" {
			return nil
		}
		st := append([]rune(a.strip), w[add:]...)
		if len(st) == 0 || !a.cond.matchStart(st) {
			return nil
		}
		return st
	}
	if add == len(w) && a.strip == "" {
		return nil
	}
	st := append(append([]rune(nil), w[:len(w)-add]...), []rune(a.strip)...)
	if len(st) == 0 || !a.cond.matchEnd(st) {
		return nil
	}
	return st
}

// suffixCheck checks w as root+suffix (pfx, if set, was already removed
// and must combine with the suffix). level 1 is the inner suffix of a
// two-suffix word: outer is the suffix stripped first.
func (d *Dict) suffixCheck(w []rune, pfx *affix, recased bool, level int) bool {
	for _, s := range d.suffixesFor(w) {
		stem := s.stem(w)
		if stem == nil {
			continue
		}
		root := string(stem)
		needOK := !s.cont.has(d.needAffix) || pfx != nil
		if pfx != nil {
			// a prefix and a suffix: both cross product, or one allows the
			// other through its continuation classes
			cross := pfx.cross && s.cross
			via := s.cont.has(pfx.flag) || pfx.cont.has(s.flag)
			if !cross && !via {
				continue
			}
			if pfx.cont.has(d.circumfix) != s.cont.has(d.circumfix) {
				continue
			}
			if via && s.cont.has(pfx.flag) {
				if d.homonymHas(root, s.flag, 0, recased) {
					return true
				}
				continue
			}
			if pfx.cont.has(s.flag) {
				if d.homonymHas(root, pfx.flag, 0, recased) {
					return true
				}
				continue
			}
			if d.homonymHas(root, s.flag, pfx.flag, recased) {
				return true
			}
			continue
		}
		if s.cont.has(d.circumfix) {
			continue
		}
		if needOK && d.homonymHas(root, s.flag, 0, recased) {
			return true
		}
		// two suffixes: stem is root+inner where inner allows s
		if level == 0 && d.twoSuffix(stem, s, recased) {
			return true
		}
	}
	return false
}

// twoSuffix checks w = root + inner, where inner's continuation classes
// include outer's flag.
func (d *Dict) twoSuffix(w []rune, outer *affix, recased bool) bool {
	for _, s := range d.suffixesFor(w) {
		if !s.cont.has(outer.flag) {
			continue
		}
		stem := s.stem(w)
		if stem == nil {
			continue
		}
		if d.homonymHas(string(stem), s.flag, 0, recased) {
			return true
		}
	}
	return false
}

// compound checks COMPOUNDRULE and COMPOUNDFLAG compounds.
func (d *Dict) compound(w string) bool {
	rw := []rune(w)
	if len(d.compoundRules) > 0 && d.ruleCompound(rw) {
		return true
	}
	if d.compoundFlag != 0 || d.compoundBegin != 0 {
		return d.flagCompound(rw, 0)
	}
	return false
}

// ruleCompound splits w into roots whose flags spell one of the rules.
func (d *Dict) ruleCompound(w []rune) bool {
	var parts []flags
	var try func(i int) bool
	try = func(i int) bool {
		if i == len(w) {
			if len(parts) < 2 {
				return false
			}
			for _, r := range d.compoundRules {
				if ruleMatch(r, parts) {
					return true
				}
			}
			return false
		}
		for j := i + d.compoundMin; j <= len(w); j++ {
			hs, ok := d.words[string(w[i:j])]
			if !ok {
				continue
			}
			for _, fs := range hs {
				use := false
				for _, f := range fs {
					if d.compoundRuleFlags[f] {
						use = true
						break
					}
				}
				if !use {
					continue
				}
				parts = append(parts, fs)
				if try(j) {
					return true
				}
				parts = parts[:len(parts)-1]
			}
		}
		return false
	}
	return try(0)
}

// ruleMatch matches part flag sets against a rule (atoms with * and ?).
func ruleMatch(rule []ruleAtom, parts []flags) bool {
	var m func(ri, pi int) bool
	m = func(ri, pi int) bool {
		if ri == len(rule) {
			return pi == len(parts)
		}
		a := rule[ri]
		switch a.rep {
		case '*':
			if m(ri+1, pi) {
				return true
			}
			return pi < len(parts) && parts[pi].has(a.f) && m(ri, pi+1)
		case '?':
			if m(ri+1, pi) {
				return true
			}
			return pi < len(parts) && parts[pi].has(a.f) && m(ri+1, pi+1)
		}
		return pi < len(parts) && parts[pi].has(a.f) && m(ri+1, pi+1)
	}
	return m(0, 0)
}

// flagCompound splits w into parts allowed at their position: the first
// with COMPOUNDFLAG or COMPOUNDBEGIN, middles with COMPOUNDFLAG or
// COMPOUNDMIDDLE, the last with COMPOUNDFLAG or COMPOUNDEND. The last part
// may carry a suffix and the first a prefix.
func (d *Dict) flagCompound(w []rune, n int) bool {
	if n > 8 {
		return false
	}
	for j := d.compoundMin; j <= len(w)-d.compoundMin; j++ {
		head := string(w[:j])
		pos := d.compoundMiddle
		if n == 0 {
			pos = d.compoundBegin
		}
		if !d.partOK(head, pos, n == 0, false) {
			continue
		}
		tail := w[j:]
		if d.partOK(string(tail), d.compoundEnd, false, true) {
			return true
		}
		if d.flagCompound(tail, n+1) {
			return true
		}
	}
	return false
}

func (d *Dict) partOK(p string, pos flag, first, last bool) bool {
	for _, fs := range d.words[p] {
		if fs.has(d.forbidden) {
			return false
		}
		if fs.has(d.compoundFlag) || fs.has(pos) {
			return true
		}
	}
	rw := []rune(p)
	if last {
		for _, s := range d.suffixesFor(rw) {
			if st := s.stem(rw); st != nil {
				for _, fs := range d.words[string(st)] {
					if fs.has(s.flag) && (fs.has(d.compoundFlag) || fs.has(pos)) {
						return true
					}
				}
			}
		}
	}
	if first {
		for _, pf := range d.prefixesFor(rw) {
			if st := pf.stem(rw); st != nil {
				for _, fs := range d.words[string(st)] {
					if fs.has(pf.flag) && (fs.has(d.compoundFlag) || fs.has(pos)) {
						return true
					}
				}
			}
		}
	}
	return false
}
