package quip

import "strings"

// Order keys (Quip's 0fa5, in quip-collab-webapp): a section's position
// sorts the document, over the letters A-Z then a-z (as bytes). A key the
// client makes ends in ":temp" until the server stores it.

// TempSuffix marks a key the client made.
const TempSuffix = ":temp"

// Between is 0fa5.generate: a key strictly between prev and next ("" for
// none), with the ":temp" suffix. Keys that aren't in order give "aaa",
// as Quip's does after its warning.
func Between(prev, next string) string { return betweenNoTemp(prev, next) + TempSuffix }

func betweenNoTemp(prev, next string) string {
	s, n := StripTemp(prev), StripTemp(next)
	if s != "" && n != "" && s >= n {
		return "aaa" // "Passed invalid parameters to order.generate_()"
	}
	if s == "" && n == "" {
		return "aaa"
	}
	if s != "" {
		if o := increment(s); orderOK(s, o, n) {
			return o
		}
	}
	if n != "" {
		if o := decrement(n); orderOK(s, o, n) {
			return o
		}
	}
	if s != "" {
		if o := s + "aaaa"; orderOK(s, o, n) {
			return o
		}
	}
	if n != "" {
		// Quip asserts that next extends prev and ends in B, but its
		// asserts only throw under Jest (fmyk.assert, CRVI.XA), so the
		// key is tried whatever next is.
		if o := n[:len(n)-1] + "Aaaaaaa"; orderOK(s, o, n) {
			return o
		}
	}
	return "aaa"
}

// nextLetter is 0fa5's next: Z is followed by a, the rest by their byte.
func nextLetter(c byte) byte {
	if c == 'Z' {
		return 'a'
	}
	return c + 1
}

func prevLetter(c byte) byte {
	if c == 'a' {
		return 'Z'
	}
	return c - 1
}

// increment: the last letter that isn't z goes up one; each z after it
// becomes B. All z: "".
func increment(e string) string {
	t := []byte(e)
	for i := len(t) - 1; i >= 0; i-- {
		if t[i] != 'z' {
			t[i] = nextLetter(t[i])
			return string(t)
		}
		t[i] = 'B'
	}
	return ""
}

// decrement: the last letter that is neither A nor B goes down one; each
// A or B after it becomes z. All A and B: "".
func decrement(e string) string {
	t := []byte(e)
	for i := len(t) - 1; i >= 0; i-- {
		if t[i] != 'A' && t[i] != 'B' {
			t[i] = prevLetter(t[i])
			return string(t)
		}
		t[i] = 'z'
	}
	return ""
}

// orderOK is 0fa5's ok: t is a key after prev (and not prev+"A") and
// before next.
func orderOK(prev, t, next string) bool {
	return t != "" && (prev == "" || prev < t) && (prev == "" || t != prev+"A") && (next == "" || t < next)
}

// StripTemp is 0fa5.stripTemp: the key without ":temp" and what follows.
func StripTemp(k string) string {
	if i := strings.Index(k, TempSuffix); i >= 0 {
		return k[:i]
	}
	return k
}

// IsTemp is 0fa5.isTemp.
func IsTemp(k string) bool { return strings.HasSuffix(k, TempSuffix) }

// PositionPath is EKJU.positionPath: the positions from the outermost
// container down to the section, joined with "-".
func PositionPath(positions ...string) string { return strings.Join(positions, "-") }
