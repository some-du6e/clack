package ui

import (
	"bufio"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"testing"
)

// What keeps an object reachable, from a heap dump
// (runtime/debug.WriteHeapDump, the "go1.7 heap dump" format of
// runtime/heapdump.go) read back in the same process: every root (a
// global, a goroutine's frame, a finalizer) whose pointers reach the
// object, and the shortest path from each. TestFinishedSessionIsCollected
// prints it when a finished session stays reachable, so its failure names
// the global or the goroutine that holds the session. The dump carries no
// object types: a path's objects are named by the symbols their words
// hold (a closure's function, an itab, a type).

type retainTarget struct {
	what string
	addr uintptr
}

// retainReport dumps the heap and names what reaches each target, or says
// why it didn't: a dump is as big as the heap, so it is written only while
// the heap is under 1 GiB (or with CLACK_RETAIN_DUMP set).
func retainReport(t *testing.T, targets []retainTarget) string {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if ms.HeapAlloc > 1<<30 && os.Getenv("CLACK_RETAIN_DUMP") == "" {
		return fmt.Sprintf("(no heap dump of what holds it: %d MiB live; CLACK_RETAIN_DUMP=1 writes one anyway)", ms.HeapAlloc>>20)
	}
	return heapRetainers(t.TempDir(), targets, 6, 30)
}

// heapRetainers writes a heap dump in dir and reads back what reaches
// each target: up to maxRoots roots each, each root's path (up to maxHops
// objects) once.
func heapRetainers(dir string, targets []retainTarget, maxRoots, maxHops int) string {
	path := filepath.Join(dir, "heap.dump")
	f, err := os.Create(path)
	if err != nil {
		return "heap dump: " + err.Error()
	}
	runtime.GC()
	debug.WriteHeapDump(f.Fd())
	if err := f.Close(); err != nil {
		return "heap dump: " + err.Error()
	}
	g, err := readHeapDump(path)
	if err != nil {
		return "heap dump: " + err.Error()
	}
	defer g.f.Close()
	var b strings.Builder
	fmt.Fprintf(&b, "what holds it, from a heap dump (%d objects, %d roots):\n", len(g.starts), len(g.roots))
	if len(g.syms.addrs) == 0 {
		// go test links its binary -s -w; go test -c keeps the symbols
		b.WriteString("(the test binary has no symbol table, so globals are bare addresses: build it with go test -c to name them)\n")
	}
	seen := map[string]bool{}
	for _, tg := range targets {
		g.retainers(&b, tg.what, uint64(tg.addr), maxRoots, maxHops, seen)
	}
	return b.String()
}

// heapGraph is a heap dump's objects, sorted by address, their pointers
// as edges between them, and the roots.
type heapGraph struct {
	starts, sizes []uint64
	cont          []int64  // where each object's contents start in the dump
	fwdAt         []uint32 // object i's pointers: fwd[fwdAt[i]:fwdAt[i+1]]
	fwd           []uint32
	revAt, rev    []uint32 // the same, reversed (retainers makes them)
	roots         []heapRoot
	syms          *symtab
	f             *os.File
}

type heapRoot struct {
	what string
	obj  uint32
	// guess: a frame's word outside its stack map (address-taken locals
	// live there), maybe a stale value
	guess bool
}

func (g *heapGraph) find(addr uint64) int {
	i := sort.Search(len(g.starts), func(i int) bool { return g.starts[i] > addr }) - 1
	if i >= 0 && addr < g.starts[i]+max(g.sizes[i], 1) {
		return i
	}
	return -1
}

type dumpReader struct {
	r   *bufio.Reader
	off int64
	err error
}

func (d *dumpReader) num() uint64 {
	var x uint64
	for s := uint(0); d.err == nil; s += 7 {
		c, err := d.r.ReadByte()
		if err != nil {
			d.err = err
			break
		}
		d.off++
		if c < 0x80 {
			return x | uint64(c)<<s
		}
		x |= uint64(c&0x7f) << s
		if s > 63 {
			d.err = errors.New("varint overflows")
		}
	}
	return 0
}

// bytes reads a length and that many bytes, into buf if it's big enough.
func (d *dumpReader) bytes(buf []byte) []byte {
	n := d.num()
	if d.err != nil {
		return buf[:0]
	}
	if n > 1<<34 {
		d.err = fmt.Errorf("a %d-byte record", n)
		return buf[:0]
	}
	if uint64(cap(buf)) < n {
		buf = make([]byte, n)
	}
	buf = buf[:n]
	if _, err := io.ReadFull(d.r, buf); err != nil {
		d.err = err
	}
	d.off += int64(n)
	return buf
}

func (d *dumpReader) str() string { return string(d.bytes(nil)) }

func (d *dumpReader) skip(n int) {
	for ; n > 0; n-- {
		d.num()
	}
}

// fields calls f with each pointer field's offset, up to the list's end.
func (d *dumpReader) fields(f func(off uint64)) {
	for d.err == nil {
		if d.num() == 0 { // fieldKindEol
			return
		}
		f(d.num())
	}
}

func dumpWord(b []byte, off uint64) uint64 {
	if off+8 > uint64(len(b)) {
		return 0
	}
	return binary.LittleEndian.Uint64(b[off:])
}

func readHeapDump(path string) (*heapGraph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	g := &heapGraph{syms: loadSymtab(), f: f}
	d := &dumpReader{r: bufio.NewReaderSize(f, 1<<20)}
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(d.r, hdr); err != nil || string(hdr) != "go1.7 heap dump\n" {
		f.Close()
		return nil, fmt.Errorf("not a heap dump (%q, %v)", hdr, err)
	}
	d.off = int64(len(hdr))
	type rawRoot struct {
		what  string
		ptr   uint64
		guess bool
	}
	var (
		starts, sizes []uint64
		cont          []int64
		ptrAt         []int
		ptrs          []uint64
		raw           []rawRoot
		finals        []uint64 // objects with a finalizer: what they point to is kept
		buf           []byte
		gname         string
		dumping       bool // the goroutine writing the dump: this test's
	)
loop:
	for d.err == nil {
		switch tag := d.num(); tag {
		case 0: // EOF
			break loop
		case 1: // object: address, contents, pointer fields
			addr := d.num()
			buf = d.bytes(buf)
			starts, sizes, cont = append(starts, addr), append(sizes, uint64(len(buf))), append(cont, d.off-int64(len(buf)))
			ptrAt = append(ptrAt, len(ptrs))
			d.fields(func(off uint64) {
				if v := dumpWord(buf, off); v != 0 {
					ptrs = append(ptrs, v)
				}
			})
		case 2: // other root
			what := d.str()
			raw = append(raw, rawRoot{what: "runtime root " + what, ptr: d.num()})
		case 3: // type
			d.skip(2)
			d.str()
			d.skip(1)
		case 4: // goroutine
			d.skip(2) // g, sp
			id := d.num()
			gopc := d.num()
			d.skip(4) // status, system, background, waitsince
			reason := d.str()
			ctxt := d.num()
			d.skip(3) // m, defer, panic
			from := "?"
			if fn := runtime.FuncForPC(uintptr(gopc)); fn != nil {
				from = shortSym(fn.Name())
			}
			gname = fmt.Sprintf("goroutine %d (%s, started by %s)", id, reason, from)
			dumping = reason == "dumping heap"
			if ctxt != 0 {
				raw = append(raw, rawRoot{what: gname + ", its context", ptr: ctxt})
			}
		case 5: // stack frame: sp, depth, child sp, contents, entry, pc, continuation pc, name, pointer fields
			d.skip(3)
			buf = d.bytes(buf)
			d.skip(3)
			what := gname + " in " + shortSym(d.str())
			mapped := map[uint64]bool{}
			d.fields(func(off uint64) {
				mapped[off] = true
				if v := dumpWord(buf, off); v != 0 {
					raw = append(raw, rawRoot{what: what, ptr: v})
				}
			})
			if !dumping {
				for off := uint64(0); off+8 <= uint64(len(buf)); off += 8 {
					if v := dumpWord(buf, off); v != 0 && !mapped[off] {
						raw = append(raw, rawRoot{what: what, ptr: v, guess: true})
					}
				}
			}
		case 6: // params
			d.skip(4)
			d.str()
			d.str()
			d.skip(1)
		case 7, 11: // finalizer, queued finalizer: object, funcval, its code, types
			obj, fn, code := d.num(), d.num(), d.num()
			d.skip(2)
			what := "a finalizer (" + g.syms.exact(code) + ")"
			raw = append(raw, rawRoot{what: what, ptr: fn})
			if tag == 11 { // queued: the object goes to the function
				raw = append(raw, rawRoot{what: what + ", queued", ptr: obj})
			} else {
				finals = append(finals, obj)
			}
		case 8: // itab
			d.skip(2)
		case 9: // OS thread
			d.skip(3)
		case 10: // memstats: 24 counters, 256 pauses, NumGC
			d.skip(24 + 256 + 1)
		case 12, 13: // data, bss: start, contents, pointer fields
			start := d.num()
			buf = d.bytes(buf)
			d.fields(func(off uint64) {
				if v := dumpWord(buf, off); v != 0 {
					raw = append(raw, rawRoot{what: "global " + g.syms.at(start+off), ptr: v})
				}
			})
		case 14: // defer: d, g, sp, pc, funcval, code, link
			d.skip(4)
			if fn := d.num(); fn != 0 {
				raw = append(raw, rawRoot{what: gname + ", a defer", ptr: fn})
			}
			d.skip(2)
		case 15: // panic: p, g, value's type, value's data, 0, link
			d.skip(3)
			if v := d.num(); v != 0 {
				raw = append(raw, rawRoot{what: gname + ", a panic's value", ptr: v})
			}
			d.skip(2)
		case 16: // memory profile bucket: id, size, frames (function, file, line), allocs, frees
			d.skip(2)
			for n := d.num(); n > 0 && d.err == nil; n-- {
				d.str()
				d.str()
				d.skip(1)
			}
			d.skip(2)
		case 17: // alloc sample
			d.skip(2)
		default:
			d.err = fmt.Errorf("record %d at %d", tag, d.off)
		}
	}
	if d.err != nil {
		f.Close()
		return nil, d.err
	}
	ptrAt = append(ptrAt, len(ptrs))
	// sorted by address
	order := make([]int, len(starts))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		switch {
		case starts[a] < starts[b]:
			return -1
		case starts[a] > starts[b]:
			return 1
		}
		return 0
	})
	n := len(order)
	g.starts, g.sizes, g.cont = make([]uint64, n), make([]uint64, n), make([]int64, n)
	for i, o := range order {
		g.starts[i], g.sizes[i], g.cont[i] = starts[o], sizes[o], cont[o]
	}
	g.fwdAt = make([]uint32, n+1)
	for i, o := range order {
		for _, v := range ptrs[ptrAt[o]:ptrAt[o+1]] {
			if j := g.find(v); j >= 0 {
				g.fwd = append(g.fwd, uint32(j))
			}
		}
		g.fwdAt[i+1] = uint32(len(g.fwd))
	}
	for _, r := range raw {
		if j := g.find(r.ptr); j >= 0 {
			g.roots = append(g.roots, heapRoot{what: r.what, obj: uint32(j), guess: r.guess})
		}
	}
	for _, obj := range finals {
		if j := g.find(obj); j >= 0 {
			for _, k := range g.fwd[g.fwdAt[j]:g.fwdAt[j+1]] {
				g.roots = append(g.roots, heapRoot{what: fmt.Sprintf("a finalizer's object (%d B)", g.sizes[j]), obj: k})
			}
		}
	}
	return g, nil
}

func (g *heapGraph) reverse() {
	n := len(g.starts)
	at := make([]uint32, n+1)
	for _, v := range g.fwd {
		at[v+1]++
	}
	for i := 1; i <= n; i++ {
		at[i] += at[i-1]
	}
	rev := make([]uint32, len(g.fwd))
	pos := slices.Clone(at[:n])
	for u := 0; u < n; u++ {
		for _, v := range g.fwd[g.fwdAt[u]:g.fwdAt[u+1]] {
			rev[pos[v]] = uint32(u)
			pos[v]++
		}
	}
	g.revAt, g.rev = at, rev
}

// retainers writes the roots that reach addr's object, up to maxRoots of
// them (the shortest paths first, a frame's unmapped words last), each
// with its path, up to maxHops objects of it, unless seen has the root (an
// earlier target's path showed it).
func (g *heapGraph) retainers(w io.Writer, what string, addr uint64, maxRoots, maxHops int, seen map[string]bool) {
	t := g.find(addr)
	if t < 0 {
		fmt.Fprintf(w, "%s (%#x): not in the dump\n", what, addr)
		return
	}
	if g.rev == nil {
		g.reverse()
	}
	// next[o]: o's pointer toward the target, -1 at the target, -2 where
	// o doesn't reach it
	next := make([]int32, len(g.starts))
	for i := range next {
		next[i] = -2
	}
	next[t] = -1
	queue := []uint32{uint32(t)}
	for i := 0; i < len(queue); i++ {
		v := queue[i]
		for _, u := range g.rev[g.revAt[v]:g.revAt[v+1]] {
			if next[u] == -2 {
				next[u] = int32(v)
				queue = append(queue, u)
			}
		}
	}
	type hit struct {
		r    heapRoot
		hops int
	}
	best := map[string]hit{}
	for _, r := range g.roots {
		if next[r.obj] == -2 {
			continue
		}
		hops := 0
		for o := int32(r.obj); o >= 0; o = next[o] {
			hops++
		}
		if h, ok := best[r.what]; !ok || (h.r.guess && !r.guess) || (h.r.guess == r.guess && hops < h.hops) {
			best[r.what] = hit{r, hops}
		}
	}
	hits := make([]hit, 0, len(best))
	for _, h := range best {
		hits = append(hits, h)
	}
	slices.SortFunc(hits, func(a, b hit) int {
		if a.r.guess != b.r.guess {
			if a.r.guess {
				return 1
			}
			return -1
		}
		if a.hops != b.hops {
			return a.hops - b.hops
		}
		return strings.Compare(a.r.what, b.r.what)
	})
	fmt.Fprintf(w, "%s (%#x, %d B) is reached from %d roots (%d objects reach it):\n", what, addr, g.sizes[t], len(hits), len(queue))
	for _, h := range hits[:min(len(hits), maxRoots)] {
		note := ""
		if h.r.guess {
			note = " (a word outside the frame's stack map: an address-taken local, or stale)"
		}
		if seen[h.r.what] {
			fmt.Fprintf(w, "  %s%s, %d hops (its path above)\n", h.r.what, note, h.hops)
			continue
		}
		seen[h.r.what] = true
		fmt.Fprintf(w, "  %s%s, %d hops:\n", h.r.what, note, h.hops)
		k := 0
		for o := int32(h.r.obj); o >= 0 && k < maxHops; o, k = next[o], k+1 {
			fmt.Fprintf(w, "    %s\n", g.describe(int(o), int(next[o])))
		}
	}
}

// describe names object o by the symbols its words hold, and which of
// its words points on to object to.
func (g *heapGraph) describe(o, to int) string {
	size := g.sizes[o]
	buf := make([]byte, min(size, 1<<16))
	if _, err := g.f.ReadAt(buf, g.cont[o]); err != nil {
		return fmt.Sprintf("%#x (%d B): %v", g.starts[o], size, err)
	}
	var hints []string
	via := ""
	for off := uint64(0); off+8 <= uint64(len(buf)); off += 8 {
		v := dumpWord(buf, off)
		if v == 0 {
			continue
		}
		if len(hints) < 4 {
			if s, ok := g.syms.byAddr[v]; ok {
				hints = append(hints, fmt.Sprintf("+%d %s", off, s))
			} else if fn := runtime.FuncForPC(uintptr(v)); fn != nil && fn.Entry() == uintptr(v) {
				hints = append(hints, fmt.Sprintf("+%d %s", off, shortSym(fn.Name()))) // a closure's code
			}
		}
		if to >= 0 && via == "" && g.find(v) == to {
			via = fmt.Sprintf(", +%d →", off)
		}
	}
	if to < 0 {
		via = ": the target"
	} else if via == "" {
		via = ", past its first 64 KiB →"
	}
	h := ""
	if len(hints) > 0 {
		h = " [" + strings.Join(hints, "; ") + "]"
	}
	return fmt.Sprintf("%#x (%d B)%s%s", g.starts[o], size, h, via)
}

// symtab is the test binary's ELF symbols (none elsewhere: addresses stay
// numbers there, functions still get their names).
type symtab struct {
	addrs, sizes []uint64
	names        []string
	byAddr       map[uint64]string
}

func loadSymtab() *symtab {
	s := &symtab{byAddr: map[uint64]string{}}
	exe, err := os.Executable()
	if err != nil {
		return s
	}
	ef, err := elf.Open(exe)
	if err != nil {
		return s
	}
	defer ef.Close()
	syms, err := ef.Symbols()
	if err != nil {
		return s
	}
	slices.SortFunc(syms, func(a, b elf.Symbol) int {
		switch {
		case a.Value < b.Value:
			return -1
		case a.Value > b.Value:
			return 1
		}
		return 0
	})
	for _, sym := range syms {
		if sym.Value == 0 || sym.Name == "" {
			continue
		}
		name := shortSym(sym.Name)
		s.addrs, s.sizes, s.names = append(s.addrs, sym.Value), append(s.sizes, sym.Size), append(s.names, name)
		if _, ok := s.byAddr[sym.Value]; !ok {
			s.byAddr[sym.Value] = name
		}
	}
	return s
}

// at names the symbol addr is in, with its offset.
func (s *symtab) at(addr uint64) string {
	i := sort.Search(len(s.addrs), func(i int) bool { return s.addrs[i] > addr }) - 1
	for ; i >= 0 && addr-s.addrs[i] < 1<<20; i-- {
		if addr < s.addrs[i]+max(s.sizes[i], 1) {
			if off := addr - s.addrs[i]; off != 0 {
				return fmt.Sprintf("%s+%d", s.names[i], off)
			}
			return s.names[i]
		}
		if s.sizes[i] != 0 {
			break
		}
	}
	return fmt.Sprintf("%#x", addr)
}

// exact names a function's code address.
func (s *symtab) exact(pc uint64) string {
	if fn := runtime.FuncForPC(uintptr(pc)); fn != nil {
		return shortSym(fn.Name())
	}
	if n, ok := s.byAddr[pc]; ok {
		return n
	}
	return fmt.Sprintf("%#x", pc)
}

func shortSym(s string) string {
	return strings.ReplaceAll(s, "github.com/some-du6e/clack/internal/", "")
}
