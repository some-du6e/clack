package quip

import (
	"encoding/hex"
	"math"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/some-du6e/clack/internal/paritytest"
)

// The parity snapshots in testdata/parity hold what Slack's own Quip
// modules give for each input (tools/oracles/quip.cjs, build
// 5718bdb1b28b); these tests give the same inputs to the Go port.

func TestQuipOrderKeysLikeSlack(t *testing.T) {
	type in struct {
		Prev *string `json:"prev"`
		Next *string `json:"next"`
	}
	s := paritytest.Load[in, string](t, "testdata/parity/quip-order.json")
	s.Run(t, paritytest.Check[in, string]{Clack: func(t *testing.T, c in) string {
		var p, n string
		if c.Prev != nil {
			p = *c.Prev
		}
		if c.Next != nil {
			n = *c.Next
		}
		return Between(p, n)
	}})
}

func TestQuipRTMLTextLikeSlack(t *testing.T) {
	type in struct {
		Fn   string `json:"fn"`
		S    string `json:"s"`
		Flag bool   `json:"flag"`
	}
	s := paritytest.Load[in, any](t, "testdata/parity/quip-rtml-text.json")
	s.Run(t, paritytest.Check[in, any]{Clack: func(t *testing.T, c in) any {
		switch c.Fn {
		case "escapeText":
			return EscapeText(c.S, c.Flag)
		case "isSafeUri":
			return IsSafeURI(c.S, c.Flag)
		}
		t.Fatalf("fn %q", c.Fn)
		return nil
	}})
}

// jsMark is a mark as Slack's getTextAndMarks gives it: value true or a
// string, offsets in UTF-16 units.
type jsMark struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type textMarks struct {
	Text  string   `json:"text"`
	Marks []jsMark `json:"marks"`
}

// utf16At is the UTF-16 offset of byte offset b in s.
func utf16At(s string, b int) int {
	n := 0
	for _, r := range s[:b] {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// byteAt is the byte offset of UTF-16 offset u in s (-1 inside a pair).
func byteAt(s string, u int) int {
	n := 0
	for i, r := range s {
		if n == u {
			return i
		}
		if n > u {
			return -1
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	if n == u {
		return len(s)
	}
	return -1
}

func toJSMarks(text string, ms []Mark) []jsMark {
	out := []jsMark{}
	for _, m := range ms {
		var v any = m.Value
		if m.Flag {
			v = true
		}
		out = append(out, jsMark{Name: m.Name, Value: v, Start: utf16At(text, m.Start), End: utf16At(text, m.End)})
	}
	return out
}

func fromJSMarks(t *testing.T, text string, ms []jsMark) []Mark {
	var out []Mark
	for _, m := range ms {
		g := Mark{Name: m.Name, Start: byteAt(text, m.Start), End: byteAt(text, m.End)}
		if g.Start < 0 || g.End < 0 {
			t.Fatalf("mark %+v cuts a character of %q", m, text)
		}
		switch v := m.Value.(type) {
		case bool:
			if !v {
				t.Fatalf("mark %+v: value false", m)
			}
			g.Flag = true
		case string:
			g.Value = v
		default:
			t.Fatalf("mark %+v: value %T", m, m.Value)
		}
		out = append(out, g)
	}
	return out
}

func TestQuipRTMLParseLikeSlack(t *testing.T) {
	type in struct {
		RTML string `json:"rtml"`
	}
	s := paritytest.Load[in, textMarks](t, "testdata/parity/quip-rtml-parse.json")
	s.Run(t, paritytest.Check[in, textMarks]{Clack: func(t *testing.T, c in) textMarks {
		text, marks, _ := ParseRTML(c.RTML)
		return textMarks{Text: text, Marks: toJSMarks(text, marks)}
	}})
}

func TestQuipRTMLWriteLikeSlack(t *testing.T) {
	type in struct {
		Text  *string  `json:"text"`
		Marks []jsMark `json:"marks"`
		RTML  *string  `json:"rtml"`
	}
	s := paritytest.Load[in, string](t, "testdata/parity/quip-rtml-write.json")
	s.Run(t, paritytest.Check[in, string]{Clack: func(t *testing.T, c in) string {
		var text string
		var marks []Mark
		switch {
		case c.RTML != nil: // Slack's round trip: parse, then write
			text, marks, _ = ParseRTML(*c.RTML)
		case c.Text != nil:
			text, marks = *c.Text, fromJSMarks(t, *c.Text, c.Marks)
		default:
			t.Fatal("no text or rtml")
		}
		out, ok := CompactRTML(text, marks)
		if !ok {
			t.Fatalf("CompactRTML refused %q %+v", text, marks)
		}
		return out
	}})
}

// messageTypes are the snapshots' type names, Slack's full names.
var messageTypes = map[string]func() any{
	"syncer.LoadData.Request":            func() any { return &LoadRequest{} },
	"syncer.FetchLatest.Request":         func() any { return &FetchRequest{} },
	"syncer.DocumentData":                func() any { return &DocumentData{} },
	"syncer.Section":                     func() any { return &Section{} },
	"syncer.Document":                    func() any { return &Document{} },
	"syncer.LoadData.ResponseWrapper":    func() any { return &LoadWrapper{} },
	"syncer.FetchLatest.Response":        func() any { return &FetchResponse{} },
	"syncer.EditDocumentResponseWrapper": func() any { return &EditAnswer{} },
}

func newMessage(t *testing.T, name string) any {
	mk, ok := messageTypes[name]
	if !ok {
		t.Fatalf("no Go type for %s", name)
	}
	return mk()
}

// build fills the struct v from a tree keyed by field number: a message
// is an object, a repeated field an array, bytes a hex string.
func build(t *testing.T, v reflect.Value, tree map[string]any) {
	t.Helper()
	l := layoutOf(v.Type())
	keys := make([]string, 0, len(tree))
	for k := range tree {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		raw := tree[k]
		num, err := strconv.Atoi(k)
		if err != nil {
			t.Fatalf("%s: field %q", v.Type().Name(), k)
		}
		f := l.byNum[num]
		if f == nil && l.extra >= 0 && l.slack[num] {
			// a field of Slack's schema Clack doesn't name: kept as its
			// record, as Unmarshal keeps it
			var w writer
			switch x := raw.(type) {
			case string:
				w.lenPrefixed(num, []byte(x))
			case bool:
				w.key(num, 0)
				if x {
					w.varint(1)
				} else {
					w.varint(0)
				}
			case float64:
				w.key(num, 0)
				w.varint(uint64(number(t, x)))
			default:
				t.Fatalf("%s.%d: a kept field of type %T", v.Type().Name(), num, raw)
			}
			e := v.Field(l.extra)
			e.SetBytes(append(e.Bytes(), w.b...))
			continue
		}
		if f == nil {
			t.Fatalf("%s has no field %d", v.Type().Name(), num)
		}
		fv := v.Field(f.idx)
		switch f.kind {
		case kBool:
			b := raw.(bool)
			fv.Set(reflect.ValueOf(&b))
		case kInt32, kInt64, kUint32:
			p := reflect.New(fv.Type().Elem())
			n := number(t, raw)
			if f.kind == kUint32 {
				p.Elem().SetUint(uint64(n))
			} else {
				p.Elem().SetInt(n)
			}
			fv.Set(p)
		case kString:
			s := raw.(string)
			fv.Set(reflect.ValueOf(&s))
		case kBytes:
			fv.SetBytes(unhex(t, raw.(string)))
		case kRaw:
			fv.Set(reflect.ValueOf(Raw(unhex(t, raw.(string)))))
		case kMsg:
			p := reflect.New(f.elem)
			build(t, p.Elem(), raw.(map[string]any))
			fv.Set(p)
		case kRepString:
			for _, x := range raw.([]any) {
				fv.Set(reflect.Append(fv, reflect.ValueOf(x.(string))))
			}
		case kRepBytes:
			for _, x := range raw.([]any) {
				fv.Set(reflect.Append(fv, reflect.ValueOf(unhex(t, x.(string)))))
			}
		case kRepRaw:
			for _, x := range raw.([]any) {
				fv.Set(reflect.Append(fv, reflect.ValueOf(Raw(unhex(t, x.(string))))))
			}
		case kRepInt32, kRepInt64, kRepBool:
			for _, x := range raw.([]any) {
				e := reflect.New(fv.Type().Elem()).Elem()
				if f.kind == kRepBool {
					e.SetBool(x.(bool))
				} else {
					e.SetInt(number(t, x))
				}
				fv.Set(reflect.Append(fv, e))
			}
		case kRepMsg, kRepMsgPtr:
			for _, x := range raw.([]any) {
				p := reflect.New(f.elem)
				build(t, p.Elem(), x.(map[string]any))
				if f.kind == kRepMsg {
					fv.Set(reflect.Append(fv, p.Elem()))
				} else {
					fv.Set(reflect.Append(fv, p))
				}
			}
		default:
			t.Fatalf("%s.%d: kind %d not in the snapshots", v.Type().Name(), num, f.kind)
		}
	}
}

func number(t *testing.T, raw any) int64 {
	f, ok := raw.(float64)
	if !ok || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		t.Fatalf("number %v", raw)
	}
	return int64(f)
}

func unhex(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestQuipWireEncodeLikeSlack(t *testing.T) {
	type in struct {
		Type   string         `json:"type"`
		Fields map[string]any `json:"fields"`
	}
	s := paritytest.Load[in, string](t, "testdata/parity/quip-wire-encode.json")
	s.Run(t, paritytest.Check[in, string]{Clack: func(t *testing.T, c in) string {
		m := newMessage(t, c.Type)
		build(t, reflect.ValueOf(m).Elem(), c.Fields)
		return hex.EncodeToString(Marshal(m))
	}})
}

// TestQuipWireReadsSlackBytes decodes what Slack's encoder wrote for each
// encode case and writes it again: every field comes back, in Slack's
// bytes.
func TestQuipWireReadsSlackBytes(t *testing.T) {
	type in struct {
		Type   string         `json:"type"`
		Fields map[string]any `json:"fields"`
	}
	s := paritytest.Load[in, string](t, "testdata/parity/quip-wire-encode.json")
	for _, c := range s.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.Slack == nil { // as paritytest.Run: a case --refresh hasn't captured yet
				t.Skip("no Slack bytes yet (tools/parity.py --refresh quip-wire-encode)")
			}
			m := newMessage(t, c.In.Type)
			if err := Unmarshal(unhex(t, *c.Slack), m); err != nil {
				t.Fatalf("Unmarshal of Slack's bytes: %v", err)
			}
			if got := hex.EncodeToString(Marshal(m)); got != *c.Slack {
				t.Errorf("read and written again:\n got %s\nwant %s", got, *c.Slack)
			}
			want := newMessage(t, c.In.Type)
			build(t, reflect.ValueOf(want).Elem(), c.In.Fields)
			if !reflect.DeepEqual(m, want) {
				t.Errorf("decoded %+v\nwant %+v", m, want)
			}
		})
	}
}

type decoded struct {
	Hex   string `json:"hex,omitempty"`
	Error string `json:"error,omitempty"`
}

func TestQuipWireDecodeLikeSlack(t *testing.T) {
	type in struct {
		Type string `json:"type"`
		Hex  string `json:"hex"`
	}
	s := paritytest.Load[in, decoded](t, "testdata/parity/quip-wire-decode.json")
	s.Run(t, paritytest.Check[in, decoded]{
		Clack: func(t *testing.T, c in) decoded {
			m := newMessage(t, c.Type)
			if err := Unmarshal(unhex(t, c.Hex), m); err != nil {
				return decoded{Error: err.Error()}
			}
			return decoded{Hex: hex.EncodeToString(Marshal(m))}
		},
		// both refuse the bytes (each says it its own way), or both
		// write the same bytes back
		Same: func(slack, clack decoded) bool {
			if slack.Error != "" || clack.Error != "" {
				return slack.Error != "" && clack.Error != ""
			}
			return slack.Hex == clack.Hex
		},
	})
}
