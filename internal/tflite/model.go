// Package tflite is a small pure-Go interpreter for TensorFlow Lite models.
// It parses the .tflite flatbuffer (schema.fbs, TFLite 2.x) and runs float32
// graphs made of the builtin operators listed in ops.go: the ones MediaPipe's
// selfie segmentation models use. It has no cgo and no runtime dependencies.
package tflite

import (
	"errors"
	"fmt"
)

// TensorType is schema.fbs TensorType.
type TensorType uint8

const (
	Float32 TensorType = 0
	Float16 TensorType = 1
	Int32   TensorType = 2
	Uint8   TensorType = 3
	Int64   TensorType = 4
	Bool    TensorType = 6
	Int16   TensorType = 7
	Int8    TensorType = 9
)

func (t TensorType) String() string {
	switch t {
	case Float32:
		return "FLOAT32"
	case Float16:
		return "FLOAT16"
	case Int32:
		return "INT32"
	case Uint8:
		return "UINT8"
	case Int64:
		return "INT64"
	case Bool:
		return "BOOL"
	case Int16:
		return "INT16"
	case Int8:
		return "INT8"
	}
	return fmt.Sprintf("TYPE%d", uint8(t))
}

// TensorInfo is one tensor of the graph as stored in the file.
type TensorInfo struct {
	Name   string
	Shape  []int
	Type   TensorType
	Buffer int    // index into Model.Buffers; 0 is the empty buffer
	Data   []byte // the constant data, nil for activations
	// Quantization (unused by float graphs, kept for the dump)
	Scale     []float32
	ZeroPoint []int64
}

// Operator is one node: a builtin code, its tensors and its options table.
type Operator struct {
	Code    BuiltinOp
	Custom  string
	Version int
	Inputs  []int // -1 is an omitted optional input
	Outputs []int
	// CustomOptions is a custom operator's option bytes.
	CustomOptions []byte
	opts          table
	hasOpts       bool
	optType       uint8
}

// Model is a parsed .tflite file (its first subgraph).
type Model struct {
	Version     int
	Description string
	Tensors     []TensorInfo
	Inputs      []int
	Outputs     []int
	Ops         []Operator
	Metadata    map[string][]byte // name → buffer contents (TFLITE_METADATA etc.)
}

// Parse reads a .tflite flatbuffer. The returned model aliases buf.
func Parse(buf []byte) (*Model, error) {
	if len(buf) >= 8 && string(buf[4:8]) != "TFL3" {
		return nil, errors.New("tflite: not a TFL3 flatbuffer")
	}
	root, err := rootTable(buf)
	if err != nil {
		return nil, err
	}
	m := &Model{Version: int(root.u32(0, 0)), Description: root.str(3)}
	codes, err := root.tables(1)
	if err != nil {
		return nil, err
	}
	bufs, err := root.tables(4)
	if err != nil {
		return nil, err
	}
	bufData := func(i int) ([]byte, error) {
		if i <= 0 || i >= len(bufs) {
			return nil, nil
		}
		b := bufs[i]
		if d := b.bytes(0); d != nil {
			return d, nil
		}
		// models over 2 GB keep data after the flatbuffer: offset, size
		off, size := b.u64(1, 0), b.u64(2, 0)
		if size == 0 {
			return nil, nil
		}
		if off <= 1 || off+size > uint64(len(buf)) {
			return nil, errBounds
		}
		return buf[off : off+size], nil
	}
	subs, err := root.tables(2)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, errors.New("tflite: no subgraphs")
	}
	sg := subs[0]
	ts, err := sg.tables(0)
	if err != nil {
		return nil, err
	}
	m.Tensors = make([]TensorInfo, len(ts))
	for i, t := range ts {
		ti := TensorInfo{
			Shape:  t.ints(0),
			Type:   TensorType(t.u8(1, 0)),
			Buffer: int(t.u32(2, 0)),
			Name:   t.str(3),
		}
		if q, ok := t.sub(4); ok {
			ti.Scale = q.floats(2)
			ti.ZeroPoint = q.i64s(3)
		}
		if ti.Data, err = bufData(ti.Buffer); err != nil {
			return nil, err
		}
		m.Tensors[i] = ti
	}
	m.Inputs = sg.ints(1)
	m.Outputs = sg.ints(2)
	ops, err := sg.tables(3)
	if err != nil {
		return nil, err
	}
	m.Ops = make([]Operator, len(ops))
	for i, o := range ops {
		ci := int(o.u32(0, 0))
		if ci >= len(codes) {
			return nil, fmt.Errorf("tflite: op %d: opcode index %d out of range", i, ci)
		}
		c := codes[ci]
		code := int32(c.u8(0, 0)) // deprecated_builtin_code
		if b := c.i32(3, 0); b > code {
			code = b
		}
		op := Operator{
			Code:    BuiltinOp(code),
			Custom:  c.str(1),
			Version: int(c.i32(2, 1)),
			Inputs:  o.ints(1),
			Outputs: o.ints(2),
			optType: o.u8(3, 0),

			CustomOptions: o.bytes(5),
		}
		op.opts, op.hasOpts = o.sub(4)
		m.Ops[i] = op
	}
	md, err := root.tables(6)
	if err != nil {
		return nil, err
	}
	m.Metadata = map[string][]byte{}
	for _, e := range md {
		d, err := bufData(int(e.u32(1, 0)))
		if err != nil {
			return nil, err
		}
		m.Metadata[e.str(0)] = d
	}
	return m, nil
}
