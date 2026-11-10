package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// EmojiListPacked is EmojiList as the store keeps it: "name\nvalue\n"
// pairs in one string (store.SetEmojiPacked). Hack Club's emoji.list is
// ~7 MB of JSON with 65k entries; decoding it into a map with
// encoding/json took ~300 ms on a CI runner, on boot's critical path.
// This scans the "emoji" object directly (one pass, one output buffer).
func (c *Client) EmojiListPacked(ctx context.Context) (string, int, error) {
	var raw json.RawMessage
	if err := c.Call(ctx, "emoji.list", nil, &raw); err != nil {
		return "", 0, err
	}
	return PackEmojiJSON(raw)
}

var errEmojiJSON = errors.New("slack: emoji.list: unexpected JSON")

// PackEmojiJSON extracts the top-level "emoji" object of an emoji.list
// answer as packed "name\nvalue\n" pairs and their count. Pairs whose
// name or value holds a newline are skipped (the packed form can't).
func PackEmojiJSON(data []byte) (string, int, error) {
	i := topKey(data, "emoji")
	if i < 0 {
		return "", 0, nil
	}
	i = skipWS(data, i)
	if i >= len(data) || data[i] != '{' {
		return "", 0, errEmojiJSON
	}
	i++
	var b strings.Builder
	b.Grow(len(data) - i)
	n := 0
	for {
		i = skipWS(data, i)
		if i >= len(data) {
			return "", 0, errEmojiJSON
		}
		if data[i] == '}' {
			return b.String(), n, nil
		}
		if data[i] == ',' {
			i++
			continue
		}
		k, j, ok := jsonString(data, i)
		if !ok {
			return "", 0, errEmojiJSON
		}
		j = skipWS(data, j)
		if j >= len(data) || data[j] != ':' {
			return "", 0, errEmojiJSON
		}
		j = skipWS(data, j+1)
		if j < len(data) && data[j] != '"' { // not a string: skip the value
			e := skipValue(data, j)
			if e < 0 {
				return "", 0, errEmojiJSON
			}
			i = e
			continue
		}
		v, e, ok := jsonString(data, j)
		if !ok {
			return "", 0, errEmojiJSON
		}
		i = e
		if strings.IndexByte(k, '\n') >= 0 || strings.IndexByte(v, '\n') >= 0 {
			continue
		}
		b.WriteString(k)
		b.WriteByte('\n')
		b.WriteString(v)
		b.WriteByte('\n')
		n++
	}
}

// topKey returns the index just after `"key":` at the top level of the
// object in data (-1 when absent).
func topKey(data []byte, key string) int {
	i := skipWS(data, 0)
	if i >= len(data) || data[i] != '{' {
		return -1
	}
	i++
	for {
		i = skipWS(data, i)
		if i >= len(data) || data[i] == '}' {
			return -1
		}
		if data[i] == ',' {
			i++
			continue
		}
		k, j, ok := jsonString(data, i)
		if !ok {
			return -1
		}
		j = skipWS(data, j)
		if j >= len(data) || data[j] != ':' {
			return -1
		}
		j++
		if k == key {
			return j
		}
		e := skipValue(data, skipWS(data, j))
		if e < 0 {
			return -1
		}
		i = e
	}
}

func skipWS(data []byte, i int) int {
	for i < len(data) && (data[i] == ' ' || data[i] == '\n' || data[i] == '\r' || data[i] == '\t') {
		i++
	}
	return i
}

// skipValue returns the index after the JSON value starting at i (-1 on
// malformed input).
func skipValue(data []byte, i int) int {
	if i >= len(data) {
		return -1
	}
	switch data[i] {
	case '"':
		_, e, ok := jsonString(data, i)
		if !ok {
			return -1
		}
		return e
	case '{', '[':
		depth := 0
		for i < len(data) {
			switch data[i] {
			case '"':
				_, e, ok := jsonString(data, i)
				if !ok {
					return -1
				}
				i = e
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
		return -1
	default:
		for i < len(data) && data[i] != ',' && data[i] != '}' && data[i] != ']' {
			i++
		}
		return i
	}
}

// jsonString decodes the JSON string starting at data[i] == '"': its value
// and the index after the closing quote. The common case (no escapes, or
// only Slack's "\/") is a slice or a quick copy.
func jsonString(data []byte, i int) (string, int, bool) {
	if i >= len(data) || data[i] != '"' {
		return "", 0, false
	}
	start := i + 1
	j := start
	esc := false
	for j < len(data) {
		c := data[j]
		if c == '"' {
			break
		}
		if c == '\\' {
			esc = true
			j += 2
			continue
		}
		j++
	}
	if j >= len(data) {
		return "", 0, false
	}
	s := data[start:j]
	if !esc {
		return string(s), j + 1, true
	}
	out := make([]byte, 0, len(s))
	for k := 0; k < len(s); k++ {
		c := s[k]
		if c != '\\' || k+1 >= len(s) {
			out = append(out, c)
			continue
		}
		k++
		switch s[k] {
		case '/', '\\', '"':
			out = append(out, s[k])
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'u':
			if k+4 >= len(s) {
				return "", 0, false
			}
			r, err := strconv.ParseUint(string(s[k+1:k+5]), 16, 32)
			if err != nil {
				return "", 0, false
			}
			k += 4
			rr := rune(r)
			if utf16Surrogate(rr) && k+6 < len(s) && s[k+1] == '\\' && s[k+2] == 'u' {
				if r2, err := strconv.ParseUint(string(s[k+3:k+7]), 16, 32); err == nil {
					rr = (rr-0xD800)<<10 + (rune(r2) - 0xDC00) + 0x10000
					k += 6
				}
			}
			out = utf8.AppendRune(out, rr)
		default:
			return "", 0, false
		}
	}
	return string(out), j + 1, true
}

func utf16Surrogate(r rune) bool { return r >= 0xD800 && r < 0xDC00 }

// okFast reports Slack's {"ok":true at the start of a body, so the
// common answer skips a full parse just to read "ok".
func okFast(data []byte) bool {
	return bytes.HasPrefix(data, []byte(`{"ok":true`))
}
