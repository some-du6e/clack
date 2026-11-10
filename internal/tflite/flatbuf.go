package tflite

import (
	"encoding/binary"
	"errors"
	"math"
)

// table is a flatbuffer table: a position in buf and its vtable.
type table struct {
	buf []byte
	pos int // the table's start
	vt  int // its vtable's start
	vtn int // vtable length in bytes
}

var errBounds = errors.New("tflite: flatbuffer out of bounds")

func u16(b []byte, i int) int   { return int(binary.LittleEndian.Uint16(b[i:])) }
func u32(b []byte, i int) int   { return int(binary.LittleEndian.Uint32(b[i:])) }
func i32(b []byte, i int) int32 { return int32(binary.LittleEndian.Uint32(b[i:])) }

func rootTable(buf []byte) (t table, err error) {
	if len(buf) < 8 {
		return t, errBounds
	}
	return tableAt(buf, u32(buf, 0))
}

func tableAt(buf []byte, pos int) (t table, err error) {
	if pos < 0 || pos+4 > len(buf) {
		return t, errBounds
	}
	vt := pos - int(i32(buf, pos))
	if vt < 0 || vt+4 > len(buf) {
		return t, errBounds
	}
	vtn := u16(buf, vt)
	if vt+vtn > len(buf) {
		return t, errBounds
	}
	return table{buf: buf, pos: pos, vt: vt, vtn: vtn}, nil
}

// field returns the absolute offset of field i, or 0 when it is absent.
func (t table) field(i int) int {
	o := 4 + 2*i
	if o+2 > t.vtn {
		return 0
	}
	off := u16(t.buf, t.vt+o)
	if off == 0 {
		return 0
	}
	return t.pos + off
}

func (t table) has(i int) bool { return t.field(i) != 0 }

func (t table) u8(i int, def uint8) uint8 {
	if p := t.field(i); p != 0 {
		return t.buf[p]
	}
	return def
}

func (t table) i32(i int, def int32) int32 {
	if p := t.field(i); p != 0 {
		return i32(t.buf, p)
	}
	return def
}

func (t table) u32(i int, def uint32) uint32 {
	if p := t.field(i); p != 0 {
		return binary.LittleEndian.Uint32(t.buf[p:])
	}
	return def
}

func (t table) u64(i int, def uint64) uint64 {
	if p := t.field(i); p != 0 {
		return binary.LittleEndian.Uint64(t.buf[p:])
	}
	return def
}

func (t table) f32(i int, def float32) float32 {
	if p := t.field(i); p != 0 {
		return math.Float32frombits(binary.LittleEndian.Uint32(t.buf[p:]))
	}
	return def
}

func (t table) b(i int) bool { return t.u8(i, 0) != 0 }

// vector returns the start and length of the vector in field i.
func (t table) vector(i int) (start, n int) {
	p := t.field(i)
	if p == 0 {
		return 0, 0
	}
	v := p + u32(t.buf, p)
	if v+4 > len(t.buf) {
		return 0, 0
	}
	return v + 4, u32(t.buf, v)
}

func (t table) str(i int) string {
	s, n := t.vector(i)
	if s+n > len(t.buf) {
		return ""
	}
	return string(t.buf[s : s+n])
}

func (t table) bytes(i int) []byte {
	s, n := t.vector(i)
	if n == 0 || s+n > len(t.buf) {
		return nil
	}
	return t.buf[s : s+n]
}

func (t table) ints(i int) []int {
	s, n := t.vector(i)
	if s+4*n > len(t.buf) {
		return nil
	}
	out := make([]int, n)
	for k := range out {
		out[k] = int(i32(t.buf, s+4*k))
	}
	return out
}

func (t table) floats(i int) []float32 {
	s, n := t.vector(i)
	if s+4*n > len(t.buf) {
		return nil
	}
	out := make([]float32, n)
	for k := range out {
		out[k] = math.Float32frombits(binary.LittleEndian.Uint32(t.buf[s+4*k:]))
	}
	return out
}

func (t table) i64s(i int) []int64 {
	s, n := t.vector(i)
	if s+8*n > len(t.buf) {
		return nil
	}
	out := make([]int64, n)
	for k := range out {
		out[k] = int64(binary.LittleEndian.Uint64(t.buf[s+8*k:]))
	}
	return out
}

// tables returns the vector of tables in field i.
func (t table) tables(i int) ([]table, error) {
	s, n := t.vector(i)
	out := make([]table, n)
	for k := range out {
		p := s + 4*k
		if p+4 > len(t.buf) {
			return nil, errBounds
		}
		tt, err := tableAt(t.buf, p+u32(t.buf, p))
		if err != nil {
			return nil, err
		}
		out[k] = tt
	}
	return out, nil
}

// sub returns the table in field i.
func (t table) sub(i int) (table, bool) {
	p := t.field(i)
	if p == 0 {
		return table{}, false
	}
	tt, err := tableAt(t.buf, p+u32(t.buf, p))
	return tt, err == nil
}
