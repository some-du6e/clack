// Package quip speaks Quip's syncer protocol, which Slack's canvas editor
// uses to load and save canvases: /canvas/-/load-data, fetch-latest and
// edit-document, with binary protobuf bodies (docs/CANVASES.md, "Slack's
// edit protocol"; the plan is docs/CANVAS-EDIT-PLAN.md).
//
// The messages are the subset of Quip's schema Clack reads or writes, with
// the field numbers of Slack's own descriptors (web client build
// 9685e6ce144b: quip-collab-webapp's QkBk and o3+F). The encoder is Slack's
// (p8gD.toBinary, c7F4.serializeToWriter): fields in ascending number, a
// set field written even when zero (proto2 presence), repeated numbers
// packed; golden_test.go holds it to bytes Slack's own encoder wrote.
package quip

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Raw is a message Clack keeps as its bytes: decoded no further, written
// back as it came.
type Raw []byte

// A field's Go type says its protobuf type:
//
//	*bool                         bool
//	*int32                        int32 or an enum (a negative one in 10 bytes, as jspb writes it)
//	*int64                        int64
//	*uint32                       uint32
//	*float32, *float64            float (fixed32), double (fixed64)
//	*string                       string
//	[]byte                        bytes (nil: unset)
//	Raw                           a message kept as bytes (nil: unset)
//	*T (a struct)                 a message
//	[]string, [][]byte, []Raw     repeated, one record each
//	[]int32, []int64, []bool      repeated, packed
//	[]T, []*T (structs)           repeated messages
//
// The tag is `pb:"N"`. A field tagged `pb:"extra"` (type []byte) keeps the
// records of fields Slack's schema has and Clack doesn't name: its
// `slack:"…"` tag lists the message's field numbers in Slack's descriptors
// ("1-11,14"), and a record of any other field is dropped, as Slack's
// decoder drops fields it doesn't know. Marshal writes the kept records
// back in field order among the named ones, as Slack's encoder would.
// `pb:"unknown"` ([]int32) lists the numbers of every field Clack doesn't
// name.

type kind uint8

const (
	kBool kind = iota + 1
	kInt32
	kInt64
	kUint32
	kFloat
	kDouble
	kString
	kBytes
	kRaw
	kMsg
	kRepString
	kRepBytes
	kRepRaw
	kRepInt32
	kRepInt64
	kRepBool
	kRepMsg
	kRepMsgPtr
)

type field struct {
	num  int
	idx  int
	kind kind
	elem reflect.Type // kMsg: the struct; kRepMsg, kRepMsgPtr: the element's struct
}

type layout struct {
	fields  []field // by number
	byNum   map[int]*field
	extra   int          // index of the `pb:"extra"` field, or -1
	slack   map[int]bool // with extra: the message's fields in Slack's schema
	unknown int          // index of the `pb:"unknown"` field, or -1
}

var (
	layoutsMu sync.Mutex
	layouts   = map[reflect.Type]*layout{}
	rawType   = reflect.TypeOf(Raw(nil))
)

func layoutOf(t reflect.Type) *layout {
	layoutsMu.Lock()
	defer layoutsMu.Unlock()
	if l, ok := layouts[t]; ok {
		return l
	}
	l := &layout{byNum: map[int]*field{}, extra: -1, unknown: -1}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag := sf.Tag.Get("pb")
		switch tag {
		case "":
			continue
		case "extra":
			l.extra = i
			l.slack = parseNumbers(t.Name(), sf.Tag.Get("slack"))
			continue
		case "unknown":
			l.unknown = i
			continue
		}
		num, err := strconv.Atoi(tag)
		if err != nil || num <= 0 || num > 1<<29-1 {
			panic(fmt.Sprintf("quip: %s.%s: bad field number %q", t.Name(), sf.Name, tag))
		}
		f := field{num: num, idx: i}
		ft := sf.Type
		switch {
		case ft == rawType:
			f.kind = kRaw
		case ft.Kind() == reflect.Ptr && ft.Elem().Kind() == reflect.Struct:
			f.kind, f.elem = kMsg, ft.Elem()
		case ft.Kind() == reflect.Ptr:
			switch ft.Elem().Kind() {
			case reflect.Bool:
				f.kind = kBool
			case reflect.Int32:
				f.kind = kInt32
			case reflect.Int64:
				f.kind = kInt64
			case reflect.Uint32:
				f.kind = kUint32
			case reflect.Float32:
				f.kind = kFloat
			case reflect.Float64:
				f.kind = kDouble
			case reflect.String:
				f.kind = kString
			}
		case ft.Kind() == reflect.Slice:
			et := ft.Elem()
			switch {
			case et.Kind() == reflect.Uint8:
				f.kind = kBytes
			case et == rawType:
				f.kind = kRepRaw
			case et.Kind() == reflect.String:
				f.kind = kRepString
			case et.Kind() == reflect.Slice && et.Elem().Kind() == reflect.Uint8:
				f.kind = kRepBytes
			case et.Kind() == reflect.Int32:
				f.kind = kRepInt32
			case et.Kind() == reflect.Int64:
				f.kind = kRepInt64
			case et.Kind() == reflect.Bool:
				f.kind = kRepBool
			case et.Kind() == reflect.Struct:
				f.kind, f.elem = kRepMsg, et
			case et.Kind() == reflect.Ptr && et.Elem().Kind() == reflect.Struct:
				f.kind, f.elem = kRepMsgPtr, et.Elem()
			}
		}
		if f.kind == 0 {
			panic(fmt.Sprintf("quip: %s.%s: no protobuf type for %s", t.Name(), sf.Name, ft))
		}
		if _, dup := l.byNum[num]; dup {
			panic(fmt.Sprintf("quip: %s: field %d twice", t.Name(), num))
		}
		l.fields = append(l.fields, f)
		l.byNum[num] = nil
	}
	sort.Slice(l.fields, func(i, j int) bool { return l.fields[i].num < l.fields[j].num })
	for i := range l.fields {
		l.byNum[l.fields[i].num] = &l.fields[i]
	}
	layouts[t] = l
	return l
}

// parseNumbers reads a `slack:"…"` list: numbers and ranges, by commas.
func parseNumbers(msg, list string) map[int]bool {
	out := map[int]bool{}
	if list == "" {
		return out
	}
	for _, part := range strings.Split(list, ",") {
		lo, hi, ok := strings.Cut(part, "-")
		a, err1 := strconv.Atoi(lo)
		b := a
		var err2 error
		if ok {
			b, err2 = strconv.Atoi(hi)
		}
		if err1 != nil || err2 != nil || a <= 0 || b < a {
			panic(fmt.Sprintf("quip: %s: bad slack field list %q", msg, list))
		}
		for n := a; n <= b; n++ {
			out[n] = true
		}
	}
	return out
}

// Marshal encodes m, a pointer to one of the package's messages.
func Marshal(m any) []byte {
	v := reflect.ValueOf(m)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		panic(fmt.Sprintf("quip: Marshal of %T", m))
	}
	var w writer
	w.message(v.Elem())
	return w.b
}

// MarshalBase64 is Marshal in standard base64 with padding, a form
// field's value (p8gD.toBinaryString: request_binary, data_binary).
func MarshalBase64(m any) string { return base64.StdEncoding.EncodeToString(Marshal(m)) }

type writer struct{ b []byte }

func (w *writer) varint(v uint64) {
	for v >= 0x80 {
		w.b = append(w.b, byte(v)|0x80)
		v >>= 7
	}
	w.b = append(w.b, byte(v))
}

func (w *writer) key(num, wt int) { w.varint(uint64(num)<<3 | uint64(wt)) }

func (w *writer) lenPrefixed(num int, b []byte) {
	w.key(num, 2)
	w.varint(uint64(len(b)))
	w.b = append(w.b, b...)
}

func (w *writer) message(v reflect.Value) {
	l := layoutOf(v.Type())
	var extra []record
	if l.extra >= 0 {
		extra = records(v.Field(l.extra).Bytes())
	}
	for i := range l.fields {
		f := &l.fields[i]
		for len(extra) > 0 && extra[0].num < f.num {
			w.b = append(w.b, extra[0].b...)
			extra = extra[1:]
		}
		fv := v.Field(f.idx)
		switch f.kind {
		case kBool, kInt32, kInt64, kUint32, kFloat, kDouble, kString, kMsg:
			if fv.IsNil() {
				continue
			}
			e := fv.Elem()
			switch f.kind {
			case kBool:
				w.key(f.num, 0)
				if e.Bool() {
					w.varint(1)
				} else {
					w.varint(0)
				}
			case kInt32, kInt64:
				w.key(f.num, 0)
				w.varint(uint64(e.Int()))
			case kUint32:
				w.key(f.num, 0)
				w.varint(e.Uint())
			case kFloat:
				w.key(f.num, 5)
				u := math.Float32bits(float32(e.Float()))
				w.b = append(w.b, byte(u), byte(u>>8), byte(u>>16), byte(u>>24))
			case kDouble:
				w.key(f.num, 1)
				u := math.Float64bits(e.Float())
				for s := 0; s < 64; s += 8 {
					w.b = append(w.b, byte(u>>s))
				}
			case kString:
				w.lenPrefixed(f.num, []byte(e.String()))
			case kMsg:
				var sub writer
				sub.message(e)
				w.lenPrefixed(f.num, sub.b)
			}
		case kBytes, kRaw:
			if fv.IsNil() {
				continue
			}
			w.lenPrefixed(f.num, fv.Bytes())
		case kRepString:
			for j := 0; j < fv.Len(); j++ {
				w.lenPrefixed(f.num, []byte(fv.Index(j).String()))
			}
		case kRepBytes, kRepRaw:
			for j := 0; j < fv.Len(); j++ {
				w.lenPrefixed(f.num, fv.Index(j).Bytes())
			}
		case kRepInt32, kRepInt64, kRepBool:
			if fv.Len() == 0 {
				continue
			}
			var p writer
			for j := 0; j < fv.Len(); j++ {
				e := fv.Index(j)
				if f.kind == kRepBool {
					if e.Bool() {
						p.varint(1)
					} else {
						p.varint(0)
					}
				} else {
					p.varint(uint64(e.Int()))
				}
			}
			w.lenPrefixed(f.num, p.b)
		case kRepMsg, kRepMsgPtr:
			for j := 0; j < fv.Len(); j++ {
				e := fv.Index(j)
				if f.kind == kRepMsgPtr {
					if e.IsNil() {
						continue
					}
					e = e.Elem()
				}
				var sub writer
				sub.message(e)
				w.lenPrefixed(f.num, sub.b)
			}
		}
	}
	for _, r := range extra {
		w.b = append(w.b, r.b...)
	}
}

// record is one field's bytes, key included, as Unmarshal kept it.
type record struct {
	num int
	b   []byte
}

// records splits kept bytes into their records, in field order (stable,
// so a repeated field keeps its order). Unmarshal only keeps whole
// records; anything else is dropped.
func records(b []byte) []record {
	var out []record
	r := &reader{b: b}
	for r.i < len(r.b) {
		start := r.i
		k, err := r.varint()
		if err != nil {
			break
		}
		if err := r.skip(int(k>>3), int(k&7), 0); err != nil {
			break
		}
		out = append(out, record{num: int(k >> 3), b: r.b[start:r.i]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].num < out[j].num })
	return out
}

// Decoding limits: an answer bigger than MaxMessage, or messages nested
// deeper than maxDepth, are refused.
const (
	MaxMessage = 16 << 20
	maxDepth   = 32
)

// ErrMalformed wraps every decoding failure.
var ErrMalformed = errors.New("quip: malformed message")

// Unmarshal decodes b into m, a pointer to one of the package's messages,
// as Slack's decoder (p8gD.fromBinary, deserializeFromReader) reads it:
// fields m doesn't model are skipped (kept in its extra field when Slack's
// schema has them), a field seen twice keeps the last value (a message
// too: the later one replaces the earlier, it doesn't merge), and a number
// may come packed (the last one counts for a single field). Invalid UTF-8
// in a string is kept as it came.
func Unmarshal(b []byte, m any) error {
	if len(b) > MaxMessage {
		return fmt.Errorf("%w: %d bytes", ErrMalformed, len(b))
	}
	v := reflect.ValueOf(m)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		panic(fmt.Sprintf("quip: Unmarshal into %T", m))
	}
	return readMessage(b, v.Elem(), 0)
}

// UnmarshalBase64 is Unmarshal of a base64 form value.
func UnmarshalBase64(s string, m any) error {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("%w: base64: %v", ErrMalformed, err)
	}
	return Unmarshal(b, m)
}

type reader struct {
	b []byte
	i int
}

func (r *reader) varint() (uint64, error) {
	var v uint64
	for s := uint(0); s <= 63; s += 7 {
		if r.i >= len(r.b) {
			return 0, fmt.Errorf("%w: truncated varint", ErrMalformed)
		}
		c := r.b[r.i]
		r.i++
		if s == 63 && c > 1 { // the tenth byte holds the 64th bit only
			break
		}
		v |= uint64(c&0x7f) << s
		if c < 0x80 {
			return v, nil
		}
	}
	return 0, fmt.Errorf("%w: varint overflow", ErrMalformed)
}

func (r *reader) take(n uint64) ([]byte, error) {
	if n > uint64(len(r.b)-r.i) {
		return nil, fmt.Errorf("%w: length %d past the end", ErrMalformed, n)
	}
	out := r.b[r.i : r.i+int(n)]
	r.i += int(n)
	return out, nil
}

// skip passes over a field of wire type wt whose key was read; groups
// (3 to the matching 4) included.
func (r *reader) skip(num, wt, depth int) error {
	switch wt {
	case 0:
		_, err := r.varint()
		return err
	case 1:
		_, err := r.take(8)
		return err
	case 2:
		n, err := r.varint()
		if err != nil {
			return err
		}
		_, err = r.take(n)
		return err
	case 5:
		_, err := r.take(4)
		return err
	case 3:
		if depth >= maxDepth {
			return fmt.Errorf("%w: groups nested too deep", ErrMalformed)
		}
		for {
			k, err := r.varint()
			if err != nil {
				return err
			}
			n, w := int(k>>3), int(k&7)
			if w == 4 {
				if n != num {
					return fmt.Errorf("%w: group %d ends as %d", ErrMalformed, num, n)
				}
				return nil
			}
			if n == 0 {
				return fmt.Errorf("%w: field number 0", ErrMalformed)
			}
			if err := r.skip(n, w, depth+1); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("%w: wire type %d", ErrMalformed, wt)
}

func readMessage(b []byte, v reflect.Value, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("%w: nested deeper than %d", ErrMalformed, maxDepth)
	}
	l := layoutOf(v.Type())
	r := &reader{b: b}
	for r.i < len(r.b) {
		start := r.i
		k, err := r.varint()
		if err != nil {
			return err
		}
		num, wt := int(k>>3), int(k&7)
		if wt == 4 {
			return fmt.Errorf("%w: group end outside a group", ErrMalformed)
		}
		f := l.byNum[num] // field 0 is no field: skipped, as Slack's decoder skips it
		if f == nil {
			if err := r.skip(num, wt, depth); err != nil {
				return err
			}
			if l.extra >= 0 && l.slack[num] {
				e := v.Field(l.extra)
				e.SetBytes(append(e.Bytes(), r.b[start:r.i]...))
			}
			if l.unknown >= 0 {
				u := v.Field(l.unknown)
				u.Set(reflect.Append(u, reflect.ValueOf(int32(num))))
			}
			continue
		}
		if err := readField(r, f, wt, v.Field(f.idx), depth); err != nil {
			return fmt.Errorf("%s.%d: %w", v.Type().Name(), num, err)
		}
	}
	return nil
}

func wantWire(wt, want int) error {
	if wt != want {
		return fmt.Errorf("%w: wire type %d, want %d", ErrMalformed, wt, want)
	}
	return nil
}

func readField(r *reader, f *field, wt int, fv reflect.Value, depth int) error {
	scalar := func() (uint64, error) {
		if err := wantWire(wt, 0); err != nil {
			return 0, err
		}
		return r.varint()
	}
	switch f.kind {
	case kBool, kInt32, kInt64, kUint32:
		var u uint64
		if wt == 2 {
			// packed, as Slack's decoder reads any number with wire type
			// 2: the last value counts, none sets nothing
			n, err := r.varint()
			if err != nil {
				return err
			}
			b, err := r.take(n)
			if err != nil {
				return err
			}
			p := &reader{b: b}
			if len(b) == 0 {
				return nil
			}
			for p.i < len(p.b) {
				if u, err = p.varint(); err != nil {
					return err
				}
			}
		} else {
			var err error
			if u, err = scalar(); err != nil {
				return err
			}
		}
		p := reflect.New(fv.Type().Elem())
		switch f.kind {
		case kBool:
			p.Elem().SetBool(u != 0)
		case kInt32:
			p.Elem().SetInt(int64(int32(u)))
		case kInt64:
			p.Elem().SetInt(int64(u))
		case kUint32:
			p.Elem().SetUint(uint64(uint32(u)))
		}
		fv.Set(p)
	case kFloat:
		if err := wantWire(wt, 5); err != nil {
			return err
		}
		b, err := r.take(4)
		if err != nil {
			return err
		}
		x := math.Float32frombits(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
		p := reflect.New(fv.Type().Elem())
		p.Elem().SetFloat(float64(x))
		fv.Set(p)
	case kDouble:
		if err := wantWire(wt, 1); err != nil {
			return err
		}
		b, err := r.take(8)
		if err != nil {
			return err
		}
		var u uint64
		for s := 0; s < 8; s++ {
			u |= uint64(b[s]) << (8 * s)
		}
		p := reflect.New(fv.Type().Elem())
		p.Elem().SetFloat(math.Float64frombits(u))
		fv.Set(p)
	case kString, kBytes, kRaw, kMsg, kRepString, kRepBytes, kRepRaw, kRepMsg, kRepMsgPtr:
		if err := wantWire(wt, 2); err != nil {
			return err
		}
		n, err := r.varint()
		if err != nil {
			return err
		}
		b, err := r.take(n)
		if err != nil {
			return err
		}
		switch f.kind {
		case kString:
			s := string(b)
			fv.Set(reflect.ValueOf(&s))
		case kBytes:
			fv.SetBytes(append([]byte{}, b...))
		case kRaw:
			fv.Set(reflect.ValueOf(Raw(append([]byte{}, b...))))
		case kRepString:
			fv.Set(reflect.Append(fv, reflect.ValueOf(string(b))))
		case kRepBytes:
			fv.Set(reflect.Append(fv, reflect.ValueOf(append([]byte{}, b...))))
		case kRepRaw:
			fv.Set(reflect.Append(fv, reflect.ValueOf(Raw(append([]byte{}, b...)))))
		case kMsg:
			// a new message each time: Slack's decoder sets a fresh
			// instance, so a field seen twice is the later one
			e := reflect.New(f.elem)
			if err := readMessage(b, e.Elem(), depth+1); err != nil {
				return err
			}
			fv.Set(e)
		case kRepMsg, kRepMsgPtr:
			e := reflect.New(f.elem)
			if err := readMessage(b, e.Elem(), depth+1); err != nil {
				return err
			}
			if f.kind == kRepMsg {
				fv.Set(reflect.Append(fv, e.Elem()))
			} else {
				fv.Set(reflect.Append(fv, e))
			}
		}
	case kRepInt32, kRepInt64, kRepBool:
		add := func(u uint64) {
			switch f.kind {
			case kRepInt32:
				fv.Set(reflect.Append(fv, reflect.ValueOf(int32(u))))
			case kRepInt64:
				fv.Set(reflect.Append(fv, reflect.ValueOf(int64(u))))
			case kRepBool:
				fv.Set(reflect.Append(fv, reflect.ValueOf(u != 0)))
			}
		}
		switch wt {
		case 0:
			u, err := r.varint()
			if err != nil {
				return err
			}
			add(u)
		case 2:
			n, err := r.varint()
			if err != nil {
				return err
			}
			b, err := r.take(n)
			if err != nil {
				return err
			}
			p := &reader{b: b}
			for p.i < len(p.b) {
				u, err := p.varint()
				if err != nil {
					return err
				}
				add(u)
			}
		default:
			return fmt.Errorf("%w: wire type %d for repeated numbers", ErrMalformed, wt)
		}
	}
	return nil
}

// ValidUTF8 reports whether every string m holds, nested ones too, is
// valid UTF-8 (TextEncoder can't send anything else, so neither may a
// save).
func ValidUTF8(m any) bool {
	return validUTF8(reflect.ValueOf(m))
}

func validUTF8(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr:
		return v.IsNil() || validUTF8(v.Elem())
	case reflect.String:
		return utf8.ValidString(v.String())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !validUTF8(v.Field(i)) {
				return false
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return true
		}
		for i := 0; i < v.Len(); i++ {
			if !validUTF8(v.Index(i)) {
				return false
			}
		}
	}
	return true
}

// Fields lists the field numbers set in m, at the top level, in order: a
// validator's view of a message's shape.
func Fields(m any) []int {
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	l := layoutOf(v.Type())
	var out []int
	for _, f := range l.fields {
		fv := v.Field(f.idx)
		set := false
		switch f.kind {
		case kBool, kInt32, kInt64, kUint32, kFloat, kDouble, kString, kMsg, kBytes, kRaw:
			set = !fv.IsNil()
		default:
			set = fv.Len() > 0
		}
		if set {
			out = append(out, f.num)
		}
	}
	return out
}

// FieldsString is Fields as "1,2,9,…".
func FieldsString(m any) string {
	var b strings.Builder
	for i, n := range Fields(m) {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}
