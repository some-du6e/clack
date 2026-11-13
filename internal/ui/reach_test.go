package ui

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
)

// reachPath is the first chain of fields, elements and map entries from
// root to a pointer into [lo, hi), or "" when root reaches none (for a
// test that finds something still reachable: what holds it). Slices are
// read to their capacity, past their length too. Closures can't be seen
// into: a func value whose code is named after one of want's methods is
// reported instead.
func reachPath(root any, lo, hi uintptr, want string) string {
	type seenKey struct {
		p uintptr
		t reflect.Type
	}
	seen := map[seenKey]bool{}
	ptrs := map[reflect.Type]bool{}
	var hasPtrs func(t reflect.Type) bool
	hasPtrs = func(t reflect.Type) bool {
		if v, ok := ptrs[t]; ok {
			return v
		}
		ptrs[t] = true // a recursive type has pointers
		v := true
		switch t.Kind() {
		case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
			reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String:
			v = false
		case reflect.Array:
			v = hasPtrs(t.Elem())
		case reflect.Struct:
			v = false
			for i := 0; i < t.NumField(); i++ {
				if hasPtrs(t.Field(i).Type) {
					v = true
					break
				}
			}
		}
		ptrs[t] = v
		return v
	}
	var walk func(v reflect.Value, path string) string
	walk = func(v reflect.Value, path string) string {
		if !v.IsValid() || !hasPtrs(v.Type()) {
			return ""
		}
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return ""
			}
			p := v.Pointer()
			if p >= lo && p < hi {
				return path
			}
			k := seenKey{p, v.Type()}
			if seen[k] {
				return ""
			}
			seen[k] = true
			return walk(v.Elem(), path)
		case reflect.UnsafePointer:
			if p := v.Pointer(); p >= lo && p < hi {
				return path + " (unsafe.Pointer)"
			}
		case reflect.Interface:
			if !v.IsNil() {
				return walk(v.Elem(), path+"<"+v.Elem().Type().String()+">")
			}
		case reflect.Struct:
			t := v.Type()
			for i := 0; i < v.NumField(); i++ {
				if r := walk(v.Field(i), path+"."+t.Field(i).Name); r != "" {
					return r
				}
			}
		case reflect.Slice:
			if v.IsNil() {
				return ""
			}
			p := v.Pointer()
			if p >= lo && p < hi {
				return path + " (a slice of it)"
			}
			k := seenKey{p, v.Type()}
			if seen[k] {
				return ""
			}
			seen[k] = true
			full := v.Slice(0, v.Cap())
			for i := 0; i < full.Len(); i++ {
				if r := walk(full.Index(i), fmt.Sprintf("%s[%d]", path, i)); r != "" {
					if i >= v.Len() {
						r += fmt.Sprintf(" (past the slice's length, %d)", v.Len())
					}
					return r
				}
			}
		case reflect.Array:
			for i := 0; i < v.Len(); i++ {
				if r := walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); r != "" {
					return r
				}
			}
		case reflect.Map:
			if v.IsNil() {
				return ""
			}
			k := seenKey{v.Pointer(), v.Type()}
			if seen[k] {
				return ""
			}
			seen[k] = true
			it := v.MapRange()
			for it.Next() {
				if r := walk(it.Key(), path+"{key}"); r != "" {
					return r
				}
				if r := walk(it.Value(), fmt.Sprintf("%s[%v]", path, it.Key())); r != "" {
					return r
				}
			}
		case reflect.Func:
			if !v.IsNil() {
				if f := runtime.FuncForPC(v.Pointer()); f != nil && strings.Contains(f.Name(), want) {
					return path + " (func " + f.Name() + ")"
				}
			}
		}
		return ""
	}
	return walk(reflect.ValueOf(root), "")
}
