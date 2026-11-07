package huddle

// A tiny protobuf wire codec: enough for Chime's SdkSignalFrame and Slack's
// PeerMessage without generated code or a protobuf dependency.

import (
	"encoding/binary"
	"errors"
	"math"
)

const (
	wtVarint = 0
	wt64     = 1
	wtBytes  = 2
	wt32     = 5
)

type pbw struct{ b []byte }

func (w *pbw) tag(field, wt int) { w.b = binary.AppendUvarint(w.b, uint64(field)<<3|uint64(wt)) }

func (w *pbw) uint(field int, v uint64) {
	w.tag(field, wtVarint)
	w.b = binary.AppendUvarint(w.b, v)
}

func (w *pbw) sint(field int, v int64) { w.uint(field, uint64(v)) } // int32/int64 (not zigzag)

func (w *pbw) boolean(field int, v bool) {
	if v {
		w.uint(field, 1)
	} else {
		w.uint(field, 0)
	}
}

func (w *pbw) bytes(field int, v []byte) {
	w.tag(field, wtBytes)
	w.b = binary.AppendUvarint(w.b, uint64(len(v)))
	w.b = append(w.b, v...)
}

func (w *pbw) str(field int, v string) {
	w.tag(field, wtBytes)
	w.b = binary.AppendUvarint(w.b, uint64(len(v)))
	w.b = append(w.b, v...)
}

// optStr writes a string field only when it is set (proto3 semantics).
func (w *pbw) optStr(field int, v string) {
	if v != "" {
		w.str(field, v)
	}
}

func (w *pbw) double(field int, v float64) {
	w.tag(field, wt64)
	w.b = binary.LittleEndian.AppendUint64(w.b, math.Float64bits(v))
}

func (w *pbw) msg(field int, f func(w *pbw)) {
	var sub pbw
	f(&sub)
	w.bytes(field, sub.b)
}

var errPB = errors.New("huddle: malformed protobuf")

// pbField is one decoded field: v for varint/fixed, b for length-delimited.
type pbField struct {
	num int
	wt  int
	v   uint64
	b   []byte
}

func (f pbField) str() string      { return string(f.b) }
func (f pbField) boolean() bool    { return f.v != 0 }
func (f pbField) u32() uint32      { return uint32(f.v) }
func (f pbField) double() float64  { return math.Float64frombits(f.v) }
func (f pbField) i64() int64       { return int64(f.v) }
func (f pbField) isBytes() bool    { return f.wt == wtBytes }
func (f pbField) clone() []byte    { return append([]byte(nil), f.b...) }
func (f pbField) float32() float32 { return math.Float32frombits(uint32(f.v)) }

// pbEach calls fn for every field of the message in b.
func pbEach(b []byte, fn func(f pbField) error) error {
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return errPB
		}
		b = b[n:]
		f := pbField{num: int(key >> 3), wt: int(key & 7)}
		switch f.wt {
		case wtVarint:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return errPB
			}
			f.v, b = v, b[n:]
		case wt64:
			if len(b) < 8 {
				return errPB
			}
			f.v, b = binary.LittleEndian.Uint64(b), b[8:]
		case wt32:
			if len(b) < 4 {
				return errPB
			}
			f.v, b = uint64(binary.LittleEndian.Uint32(b)), b[4:]
		case wtBytes:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return errPB
			}
			f.b, b = b[n:n+int(l)], b[n+int(l):]
		default:
			return errPB
		}
		if err := fn(f); err != nil {
			return err
		}
	}
	return nil
}

// pbUints appends a repeated uint32 field, packed or not.
func pbUints(dst []uint32, f pbField) []uint32 {
	if f.wt != wtBytes {
		return append(dst, uint32(f.v))
	}
	b := f.b
	for len(b) > 0 {
		v, n := binary.Uvarint(b)
		if n <= 0 {
			break
		}
		dst = append(dst, uint32(v))
		b = b[n:]
	}
	return dst
}
