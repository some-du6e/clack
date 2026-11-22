package chart

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// The block's chart arrives as JSON and Slack's renderable reads it as
// JavaScript values: String(label), typeof value === "number", for...of
// over data, React children for titles and legend labels. These helpers
// are those semantics over encoding/json's values (nil, bool, float64,
// string, []any, map[string]any).

// errRender is a render that throws in Slack's renderable: the block's
// SUIErrorBoundary draws the surface's errorFallback instead.
var errRender = errors.New("data_visualization: render error")

// jsString is String(v) for a JSON value (present false: undefined).
func jsString(v any, present bool) string {
	if !present {
		return "undefined"
	}
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return JSNumber(t)
	case []any:
		// Array.prototype.join: null and undefined items are ""
		parts := make([]string, len(t))
		for i, x := range t {
			if x != nil {
				parts[i] = jsString(x, true)
			}
		}
		return strings.Join(parts, ",")
	case map[string]any:
		return "[object Object]"
	}
	return ""
}

// JSNumber is Number.prototype.toString(): the shortest digits that read
// back as f, in exponent form below 1e-6 and from 1e21.
func JSNumber(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	neg := f < 0
	if neg {
		f = -f
	}
	// shortest digits d1..dk and the exponent n: f = 0.d1..dk × 10^n
	e := strconv.FormatFloat(f, 'e', -1, 64) // d.ddde±XX
	mant, expStr, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, _ := strconv.Atoi(expStr)
	n := exp + 1
	k := len(digits)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	switch {
	case k <= n && n <= 21:
		b.WriteString(digits)
		b.WriteString(strings.Repeat("0", n-k))
	case 0 < n && n <= 21:
		b.WriteString(digits[:n])
		b.WriteByte('.')
		b.WriteString(digits[n:])
	case -6 < n && n <= 0:
		b.WriteString("0.")
		b.WriteString(strings.Repeat("0", -n))
		b.WriteString(digits)
	default:
		b.WriteByte(digits[0])
		if k > 1 {
			b.WriteByte('.')
			b.WriteString(digits[1:])
		}
		b.WriteByte('e')
		if n-1 >= 0 {
			b.WriteByte('+')
		}
		b.WriteString(strconv.Itoa(n - 1))
	}
	return b.String()
}

// toChartNumber is mI93's z: a finite number, else null (NaN here).
func toChartNumber(v any) float64 {
	if f, ok := v.(float64); ok && !math.IsInf(f, 0) && !math.IsNaN(f) {
		return f
	}
	return math.NaN()
}

// reactText is what React draws for v as a child: strings and numbers
// as text, null, undefined and booleans as nothing, arrays item by
// item; an object throws ("Objects are not valid as a React child").
func reactText(v any, present bool) (string, error) {
	if !present {
		return "", nil
	}
	switch t := v.(type) {
	case nil, bool:
		return "", nil
	case string:
		return t, nil
	case float64:
		return JSNumber(t), nil
	case []any:
		var b strings.Builder
		for _, x := range t {
			s, err := reactText(x, true)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		}
		return b.String(), nil
	}
	return "", errRender
}

// iterate is for...of over v: an array's items, a string's characters
// (as values with no fields); anything else throws (not iterable).
func iterate(v any, present bool) ([]any, error) {
	if !present {
		return nil, errRender
	}
	switch t := v.(type) {
	case []any:
		return t, nil
	case string:
		out := make([]any, 0, len(t))
		for _, r := range t {
			out = append(out, string(r))
		}
		return out, nil
	}
	return nil, errRender
}

// arrayOf is v when it is an array (Array.prototype.map and friends);
// anything else throws (a string has no .map).
func arrayOf(v any, present bool) ([]any, error) {
	if a, ok := v.([]any); ok && present {
		return a, nil
	}
	return nil, errRender
}

// field is o[key] for a value Slack reads a property of: undefined
// (present false) for a missing key or a primitive's property (a string's
// "label" is undefined); null and undefined themselves throw on a property
// read (TypeError).
func field(o any, present bool, key string) (any, bool, error) {
	if !present || o == nil {
		return nil, false, errRender
	}
	m, ok := o.(map[string]any)
	if !ok {
		return nil, false, nil
	}
	// Slack camelCases every key first (toCamelCaseProps): the block's
	// snake_case keys and their camelCase spellings are the same key
	if v, ok := m[key]; ok {
		return v, true, nil
	}
	if c := camelKey(key); c != key {
		if v, ok := m[c]; ok {
			return v, true, nil
		}
	}
	return nil, false, nil
}

// camelKey is lodash's camelCase for a snake_case key.
func camelKey(k string) string {
	if !strings.Contains(k, "_") {
		return k
	}
	var b strings.Builder
	up := false
	for _, r := range k {
		if r == '_' {
			up = b.Len() > 0
			continue
		}
		if up && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		up = false
		b.WriteRune(r)
	}
	return b.String()
}

// jsKey is a value as a JS Map/Set key: Slack's duplicate checks compare
// with SameValueZero, so "1" and 1 differ and every object is its own.
type jsKey struct {
	kind byte // 's' string, 'n' number, 'b' bool, '0' null, 'u' undefined, 'o' object
	s    string
	n    float64
	id   int
}

func keyOf(v any, present bool, id int) jsKey {
	if !present {
		return jsKey{kind: 'u'}
	}
	switch t := v.(type) {
	case nil:
		return jsKey{kind: '0'}
	case string:
		return jsKey{kind: 's', s: t}
	case float64:
		if math.IsNaN(t) {
			return jsKey{kind: 'n', s: "NaN"}
		}
		if t == 0 {
			t = 0 // -0 and +0 are one key
		}
		return jsKey{kind: 'n', n: t}
	case bool:
		if t {
			return jsKey{kind: 'b', s: "t"}
		}
		return jsKey{kind: 'b', s: "f"}
	}
	return jsKey{kind: 'o', id: id}
}
