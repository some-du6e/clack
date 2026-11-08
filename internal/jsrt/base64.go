package jsrt

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

// atobGo is the HTML spec's forgiving-base64 decode, returning the bytes
// as a Latin-1 string (each byte one code point); ok is false where atob
// throws InvalidCharacterError. It runs natively: atob/btoa in JS were
// slow enough (a 400 KB string took seconds) to run plugins' start-up or
// send checks past the task timeout.
func atobGo(s string) (string, bool) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\f', '\r', ' ':
			return -1
		}
		return r
	}, s)
	if len(s)%4 == 0 {
		if strings.HasSuffix(s, "==") {
			s = s[:len(s)-2]
		} else if strings.HasSuffix(s, "=") {
			s = s[:len(s)-1]
		}
	}
	if len(s)%4 == 1 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/') {
			return "", false
		}
	}
	b, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return "", false
	}
	var sb strings.Builder
	sb.Grow(len(b) * 2)
	for _, x := range b {
		sb.WriteRune(rune(x))
	}
	return sb.String(), true
}

// btoaGo base64-encodes a Latin-1 string; ok is false (InvalidCharacterError)
// when a code point is above U+00FF.
func btoaGo(s string) (string, bool) {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r > 0xff || (r == utf8.RuneError && n <= 1) {
			return "", false
		}
		b = append(b, byte(r))
		i += n
	}
	return base64.StdEncoding.EncodeToString(b), true
}
