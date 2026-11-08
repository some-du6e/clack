package theme

import (
	"image/color"
	"sync"
	"time"
)

// The account theme's fields are computed once per (theme, mode,
// gradients) and kept: the UI re-applies the theme on every refresh (mode
// switch, stylesheet change, prefs event) and Appearance draws every
// preset each frame, and a custom-hex theme costs ~250 HCT solves. The
// computation runs on its own goroutine; the UI waits for it only briefly
// (FieldsAsync), so even a pathological theme can't hold a frame.

type fieldKey struct {
	t          IATheme
	dark, grad bool
}

type fieldEntry struct {
	done   chan struct{}
	f      map[string]color.NRGBA
	panic  any
	readys []func()
}

const fieldCacheSize = 128

var fieldCache struct {
	sync.Mutex
	m     map[fieldKey]*fieldEntry
	order []fieldKey
}

// fieldsEntry returns the entry for k, starting its computation if new.
func fieldsEntry(k fieldKey) *fieldEntry {
	fieldCache.Lock()
	defer fieldCache.Unlock()
	if fieldCache.m == nil {
		fieldCache.m = map[fieldKey]*fieldEntry{}
	}
	if e := fieldCache.m[k]; e != nil {
		return e
	}
	e := &fieldEntry{done: make(chan struct{})}
	fieldCache.m[k] = e
	fieldCache.order = append(fieldCache.order, k)
	if len(fieldCache.order) > fieldCacheSize {
		delete(fieldCache.m, fieldCache.order[0])
		fieldCache.order = fieldCache.order[1:]
	}
	go func() {
		var f map[string]color.NRGBA
		func() {
			defer func() {
				if r := recover(); r != nil {
					e.panic = r
				}
			}()
			f = k.t.Fields(k.dark, k.grad)
		}()
		if f == nil { // a theme that can't be mapped (a bug): Slack's default
			f = DefaultIATheme.Fields(k.dark, true)
		}
		fieldCache.Lock()
		e.f = f
		rs := e.readys
		e.readys = nil
		close(e.done)
		fieldCache.Unlock()
		for _, r := range rs {
			r()
		}
	}()
	return e
}

// FieldsAsync is t.Fields(dark, gradients) from the cache. If it isn't
// computed within wait, ok is false and ready (when not nil) is called
// once, on another goroutine, when it is. The map is shared: don't modify
// it.
func FieldsAsync(t IATheme, dark, gradients bool, wait time.Duration, ready func()) (map[string]color.NRGBA, bool) {
	e := fieldsEntry(fieldKey{t, dark, gradients})
	select {
	case <-e.done:
		return e.f, true
	default:
	}
	if wait > 0 {
		tm := time.NewTimer(wait)
		select {
		case <-e.done:
			tm.Stop()
			return e.f, true
		case <-tm.C:
		}
	}
	if ready != nil {
		fieldCache.Lock()
		select {
		case <-e.done:
			fieldCache.Unlock()
			go ready()
		default:
			e.readys = append(e.readys, ready)
			fieldCache.Unlock()
		}
	}
	return nil, false
}

// CachedFields is t.Fields from the cache, waiting for it (tests, and
// Slack's default theme, which has no custom colours).
func CachedFields(t IATheme, dark, gradients bool) map[string]color.NRGBA {
	e := fieldsEntry(fieldKey{t, dark, gradients})
	<-e.done
	return e.f
}

// ApplyIAFields is ApplyIATheme with the fields already computed.
func ApplyIAFields(p *Palette, f map[string]color.NRGBA, dark bool) {
	def := CachedFields(DefaultIATheme, dark, true)
	for k, v := range f {
		if def[k] != v {
			if set := accountFields[k]; set != nil {
				*set(p) = v
			}
		}
	}
}
