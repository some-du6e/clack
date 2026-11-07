package slackc

import (
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"unsafe"
)

// Props are a Slack component's props, Slack-shaped (the names and shapes
// the web client's React components take; see docs/PLUGINS-components.md).
// Values are JSON-like: string, bool, float64/int/int64, nil, Props /
// map[string]any, []any (or typed slices), plus Go funcs (they cross to JS
// as callbacks) and, in props decoded from JS, JSFunc and Element.
//
// A nil Props is valid everywhere and means "no overrides".
type Props map[string]any

// JSFunc is a JS function prop ({"$fn":id}); call it with Call.
type JSFunc struct{ ID int64 }

// Call invokes the JS function with JSON-encodable args (async).
func (f JSFunc) Call(args ...any) {
	if b := bridge(); b != nil && f.ID != 0 {
		b.Call(f.ID, encodeArgs(args))
	}
}

// Element is a React element passed as a prop ({"$el":[…]}), e.g. a
// Tooltip's tip; lay it out with Host.Element.
type Element struct{ Nodes []*Node }

// Str returns a string prop.
func (p Props) Str(k string) (string, bool) {
	s, ok := p[k].(string)
	return s, ok
}

// StrOr returns a string prop or def.
func (p Props) StrOr(k, def string) string {
	if s, ok := p[k].(string); ok {
		return s
	}
	return def
}

// Num returns a numeric prop as float64 (JSON numbers, Go ints and floats).
func (p Props) Num(k string) (float64, bool) {
	return toFloat(p[k])
}

// IntOr returns a numeric prop as an int or def.
func (p Props) IntOr(k string, def int) int {
	if f, ok := toFloat(p[k]); ok {
		return int(f)
	}
	return def
}

// Bool returns a boolean prop (JS truthiness for non-bools).
func (p Props) Bool(k string) bool {
	return truthy(p[k])
}

// Has reports whether the prop is present (even if null).
func (p Props) Has(k string) bool {
	_, ok := p[k]
	return ok
}

// Map returns an object prop.
func (p Props) Map(k string) Props {
	switch m := p[k].(type) {
	case Props:
		return m
	case map[string]any:
		return Props(m)
	}
	return nil
}

// List returns an array prop.
func (p Props) List(k string) []any {
	switch v := p[k].(type) {
	case []any:
		return v
	case nil:
		return nil
	}
	rv := reflect.ValueOf(p[k])
	if rv.Kind() == reflect.Slice {
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
		return out
	}
	return nil
}

// Func returns a function prop: a JSFunc, or a Go func the native passed
// in that came back through <Original>.
func (p Props) Func(k string) (any, bool) {
	switch v := p[k].(type) {
	case JSFunc:
		return v, true
	case nil:
		return nil, false
	}
	if reflect.ValueOf(p[k]).Kind() == reflect.Func {
		return p[k], true
	}
	return nil, false
}

// Invoke calls a function prop (JS or Go) with args. Go funcs run on the
// calling goroutine.
func (p Props) Invoke(k string, args ...any) {
	switch f := p[k].(type) {
	case JSFunc:
		f.Call(args...)
	case nil:
	default:
		callGo(f, args)
	}
}

// Elem returns a React-element prop.
func (p Props) Elem(k string) (Element, bool) {
	e, ok := p[k].(Element)
	return e, ok
}

func toFloat(v any) (float64, bool) {
	switch v := v.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0 && !math.IsNaN(v)
	case int:
		return v != 0
	case int64:
		return v != 0
	}
	return true
}

// --- JSON decoding ($fn / $gofn / $el) ---

func decodeValue(raw json.RawMessage) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return revive(v)
}

// revive turns {"$fn"}, {"$gofn"} and {"$el"} markers into JSFunc, the
// original Go func, and Element.
func revive(v any) (any, error) {
	switch v := v.(type) {
	case map[string]any:
		if len(v) == 1 {
			if id, ok := v["$fn"].(float64); ok {
				return JSFunc{ID: int64(id)}, nil
			}
			if id, ok := v["$gofn"].(float64); ok {
				if f := goFn(int64(id)); f != nil {
					return f, nil
				}
				return nil, nil
			}
			if el, ok := v["$el"]; ok {
				b, err := json.Marshal(el)
				if err != nil {
					return nil, err
				}
				nodes, err := ParseTree(b)
				if err != nil {
					return nil, err
				}
				return Element{Nodes: nodes}, nil
			}
		}
		out := make(Props, len(v))
		for k, x := range v {
			r, err := revive(x)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		for i, x := range v {
			r, err := revive(x)
			if err != nil {
				return nil, err
			}
			v[i] = r
		}
		return v, nil
	}
	return v, nil
}

// DecodeProps parses a JSON props object from JS.
func DecodeProps(b []byte) (Props, error) {
	v, err := decodeValue(b)
	if err != nil {
		return nil, err
	}
	p, _ := v.(Props)
	return p, nil
}

// --- Go funcs crossing to JS ---

var gofns struct {
	sync.Mutex
	next int64
	m    map[int64]any
}

func goFn(id int64) any {
	gofns.Lock()
	defer gofns.Unlock()
	return gofns.m[id]
}

// fnSlot gives a Go func a callback id. A root reuses its ids across
// renders (slot per prop path), so the JSON stays stable while the newest
// closure is the one called.
func fnSlot(slots map[string]int64, path string, f any) int64 {
	gofns.Lock()
	defer gofns.Unlock()
	if gofns.m == nil {
		gofns.m = map[int64]any{}
	}
	id, ok := slots[path]
	if !ok {
		gofns.next++
		id = gofns.next
		slots[path] = id
	}
	gofns.m[id] = f
	return id
}

func releaseSlots(slots map[string]int64) {
	gofns.Lock()
	for _, id := range slots {
		delete(gofns.m, id)
	}
	gofns.Unlock()
}

// callGo calls a Go func with JSON-ish args, adapting common signatures.
func callGo(f any, args []any) any {
	arg := func(i int) any {
		if i < len(args) {
			return args[i]
		}
		return nil
	}
	switch f := f.(type) {
	case func():
		f()
	case func(any):
		f(arg(0))
	case func(...any):
		f(args...)
	case func([]any) any:
		return f(args)
	case func(...any) any:
		return f(args...)
	case func(string):
		s, _ := arg(0).(string)
		f(s)
	case func(bool):
		f(truthy(arg(0)))
	case func(Props):
		p, _ := arg(0).(Props)
		f(p)
	case func(float64):
		x, _ := toFloat(arg(0))
		f(x)
	default:
		rv := reflect.ValueOf(f)
		if rv.Kind() != reflect.Func {
			return nil
		}
		t := rv.Type()
		in := make([]reflect.Value, t.NumIn())
		for i := range in {
			pt := t.In(i)
			a := arg(i)
			av := reflect.ValueOf(a)
			if a != nil && av.Type().ConvertibleTo(pt) {
				in[i] = av.Convert(pt)
			} else {
				in[i] = reflect.Zero(pt)
			}
		}
		if t.IsVariadic() {
			out := rv.CallSlice(in)
			if len(out) > 0 {
				return out[0].Interface()
			}
			return nil
		}
		out := rv.Call(in)
		if len(out) > 0 {
			return out[0].Interface()
		}
	}
	return nil
}

// --- JSON encoding (Go → JS) ---

// encodeProps encodes props for JS. Go funcs get stable ids from slots;
// "$key" is dropped.
func encodeProps(p Props, slots map[string]int64) []byte {
	b := make([]byte, 0, 256)
	return appendValue(b, map[string]any(p), slots, "")
}

func encodeArgs(args []any) []byte {
	b := []byte{'['}
	for i, a := range args {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendValue(b, a, nil, "")
	}
	return append(b, ']')
}

func appendValue(b []byte, v any, slots map[string]int64, path string) []byte {
	switch v := v.(type) {
	case nil:
		return append(b, "null"...)
	case string:
		return appendString(b, v)
	case bool:
		return strconv.AppendBool(b, v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return append(b, "null"...)
		}
		return strconv.AppendFloat(b, v, 'g', -1, 64)
	case float32:
		return appendValue(b, float64(v), slots, path)
	case int:
		return strconv.AppendInt(b, int64(v), 10)
	case int64:
		return strconv.AppendInt(b, v, 10)
	case int32:
		return strconv.AppendInt(b, int64(v), 10)
	case uint64:
		return strconv.AppendUint(b, v, 10)
	case JSFunc:
		b = append(b, `{"$fn":`...)
		b = strconv.AppendInt(b, v.ID, 10)
		return append(b, '}')
	case Element:
		return append(b, "null"...) // elements don't cross back to JS
	case Props:
		return appendMap(b, v, slots, path)
	case map[string]any:
		return appendMap(b, v, slots, path)
	case []any:
		b = append(b, '[')
		for i, x := range v {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendValue(b, x, slots, path+"."+strconv.Itoa(i))
		}
		return append(b, ']')
	case []string:
		b = append(b, '[')
		for i, x := range v {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendString(b, x)
		}
		return append(b, ']')
	case json.RawMessage:
		return append(b, v...)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Func:
		if slots == nil {
			return append(b, "null"...)
		}
		b = append(b, `{"$gofn":`...)
		b = strconv.AppendInt(b, fnSlot(slots, path, v), 10)
		return append(b, '}')
	case reflect.Slice, reflect.Array:
		b = append(b, '[')
		for i := 0; i < rv.Len(); i++ {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendValue(b, rv.Index(i).Interface(), slots, path+"."+strconv.Itoa(i))
		}
		return append(b, ']')
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			m := make(map[string]any, rv.Len())
			it := rv.MapRange()
			for it.Next() {
				m[it.Key().String()] = it.Value().Interface()
			}
			return appendMap(b, m, slots, path)
		}
	case reflect.Pointer:
		if rv.IsNil() {
			return append(b, "null"...)
		}
	}
	js, err := json.Marshal(v)
	if err != nil {
		return append(b, "null"...)
	}
	return append(b, js...)
}

func appendString(b []byte, s string) []byte {
	js, _ := json.Marshal(s)
	return append(b, js...)
}

func appendMap(b []byte, m map[string]any, slots map[string]int64, path string) []byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k == "$key" && path == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b = append(b, '{')
	for i, k := range keys {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendString(b, k)
		b = append(b, ':')
		b = appendValue(b, m[k], slots, path+"."+k)
	}
	return append(b, '}')
}

// --- hashing (no allocation) ---

const (
	fnvOff   = 14695981039346656037
	fnvPrime = 1099511628211
)

func hstr(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime
	}
	return h
}

func mix(h uint64) uint64 {
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

// hashProps is an order-independent hash of a props tree. Go funcs hash
// as their prop path only (closures are new every frame); "$key" is
// excluded at the top level.
func hashProps(p Props) uint64 {
	var h uint64
	for k, v := range p {
		if k == "$key" {
			continue
		}
		h += mix(hashValue(hstr(fnvOff, k), v))
	}
	return mix(h ^ uint64(len(p)))
}

func hashValue(h uint64, v any) uint64 {
	switch v := v.(type) {
	case nil:
		return h*fnvPrime ^ 1
	case string:
		return hstr(h^2, v)
	case bool:
		if v {
			return h*fnvPrime ^ 3
		}
		return h*fnvPrime ^ 4
	case float64:
		return (h ^ math.Float64bits(v)) * fnvPrime
	case float32:
		return (h ^ math.Float64bits(float64(v))) * fnvPrime
	case int:
		return (h ^ math.Float64bits(float64(v))) * fnvPrime
	case int64:
		return (h ^ math.Float64bits(float64(v))) * fnvPrime
	case int32:
		return (h ^ math.Float64bits(float64(v))) * fnvPrime
	case JSFunc:
		return (h ^ uint64(v.ID) ^ 0x9e3779b97f4a7c15) * fnvPrime
	case Props:
		var s uint64
		for k, x := range v {
			s += mix(hashValue(hstr(fnvOff, k), x))
		}
		return mix(h ^ s ^ 5)
	case map[string]any:
		var s uint64
		for k, x := range v {
			s += mix(hashValue(hstr(fnvOff, k), x))
		}
		return mix(h ^ s ^ 5)
	case []any:
		h ^= 6
		for _, x := range v {
			h = mix(hashValue(h, x))
		}
		return h
	case []string:
		h ^= 7
		for _, x := range v {
			h = mix(hstr(h, x))
		}
		return h
	case Element:
		if len(v.Nodes) > 0 {
			return (h ^ uint64(uintptr(unsafe.Pointer(v.Nodes[0])))) * fnvPrime
		}
		return h ^ 8
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Func:
		return h*fnvPrime ^ 9
	case reflect.Slice, reflect.Array:
		h ^= 10
		for i := 0; i < rv.Len(); i++ {
			h = mix(hashValue(h, rv.Index(i).Interface()))
		}
		return h
	case reflect.Map:
		var s uint64
		it := rv.MapRange()
		for it.Next() {
			s += mix(hashValue(hstr(fnvOff, it.Key().String()), it.Value().Interface()))
		}
		return mix(h ^ s ^ 11)
	case reflect.Pointer:
		if rv.IsNil() {
			return h ^ 12
		}
		return hashValue(h, rv.Elem().Interface())
	case reflect.Struct:
		h ^= 13
		for i := 0; i < rv.NumField(); i++ {
			if rv.Type().Field(i).IsExported() {
				h = mix(hashValue(h, rv.Field(i).Interface()))
			}
		}
		return h
	case reflect.String:
		return hstr(h^2, rv.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return (h ^ uint64(rv.Int())) * fnvPrime
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return (h ^ rv.Uint()) * fnvPrime
	case reflect.Bool:
		if rv.Bool() {
			return h*fnvPrime ^ 3
		}
		return h*fnvPrime ^ 4
	case reflect.Float32, reflect.Float64:
		return (h ^ math.Float64bits(rv.Float())) * fnvPrime
	}
	return h
}

func isFunc(v any) bool { return v != nil && reflect.ValueOf(v).Kind() == reflect.Func }
