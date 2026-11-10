package tflite

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
)

// Options configure an Interpreter.
type Options struct {
	// Threads is how many goroutines the big kernels split their rows
	// over; 0 means min(GOMAXPROCS, 4).
	Threads int
	// KeepAll gives every tensor its own buffer (no reuse), so Tensor can
	// read intermediate results after Invoke. Tests use it.
	KeepAll bool
}

// Interpreter runs one Model. It is not safe for concurrent use: Invoke
// writes the shared activation buffers.
type Interpreter struct {
	m       *Model
	data    [][]float32 // per tensor: constant data, or its activation buffer
	shapes  [][]int
	steps   []step
	pool    *workers
	keepAll bool
}

type step struct {
	op  BuiltinOp
	idx int
	run func()
}

// NewInterpreter prepares m to run: it decodes the constants (folding
// DEQUANTIZE of float16 weights into float32), checks every operator is
// supported, and plans the activation buffers.
func NewInterpreter(m *Model, o Options) (*Interpreter, error) {
	threads := o.Threads
	if threads <= 0 {
		threads = min(runtime.GOMAXPROCS(0), 4)
	}
	it := &Interpreter{m: m, data: make([][]float32, len(m.Tensors)), shapes: make([][]int, len(m.Tensors)),
		pool: newWorkers(threads), keepAll: o.KeepAll}
	isConst := make([]bool, len(m.Tensors))
	for i, t := range m.Tensors {
		it.shapes[i] = t.Shape
		if t.Data == nil {
			continue
		}
		isConst[i] = true
		switch t.Type {
		case Float32:
			it.data[i] = decodeF32(t.Data)
		case Float16:
			it.data[i] = decodeF16(t.Data)
		case Int32:
			// shape/axis operands: kept as float32 views for uniformity;
			// ints() reads the raw bytes instead
		default:
			return nil, fmt.Errorf("tflite: tensor %d %q: constant type %s not supported", i, t.Name, t.Type)
		}
	}
	for _, i := range m.Inputs {
		if m.Tensors[i].Type != Float32 {
			return nil, fmt.Errorf("tflite: input %q is %s, only FLOAT32 graphs are supported", m.Tensors[i].Name, m.Tensors[i].Type)
		}
	}
	// fold DEQUANTIZE of constants; everything else becomes a step
	var ops []int
	for k, op := range m.Ops {
		if op.Code == OpDequantize && len(op.Inputs) == 1 && isConst[op.Inputs[0]] {
			in, out := op.Inputs[0], op.Outputs[0]
			if m.Tensors[in].Type != Float16 && m.Tensors[in].Type != Float32 {
				return nil, fmt.Errorf("tflite: op %d: DEQUANTIZE of %s not supported", k, m.Tensors[in].Type)
			}
			it.data[out] = it.data[in]
			isConst[out] = true
			continue
		}
		ops = append(ops, k)
	}
	// liveness: the last step that reads each activation
	last := make([]int, len(m.Tensors))
	for i := range last {
		last[i] = -1
	}
	for s, k := range ops {
		for _, i := range m.Ops[k].Inputs {
			if i >= 0 {
				last[i] = s
			}
		}
	}
	for _, i := range m.Outputs {
		last[i] = len(ops)
	}
	var free [][]float32
	alloc := func(n int) []float32 {
		if !it.keepAll {
			best := -1
			for j, b := range free {
				if cap(b) >= n && (best < 0 || cap(b) < cap(free[best])) {
					best = j
				}
			}
			if best >= 0 {
				b := free[best][:n]
				free = append(free[:best], free[best+1:]...)
				return b
			}
		}
		return make([]float32, n)
	}
	for _, i := range m.Inputs {
		it.data[i] = alloc(numElems(m.Tensors[i].Shape))
	}
	for s, k := range ops {
		op := m.Ops[k]
		for _, o := range op.Outputs {
			if isConst[o] {
				return nil, fmt.Errorf("tflite: op %d writes a constant", k)
			}
			if it.data[o] == nil {
				it.data[o] = alloc(numElems(m.Tensors[o].Shape))
			}
		}
		for _, i := range op.Inputs {
			if i >= 0 && !isConst[i] && it.data[i] == nil {
				return nil, fmt.Errorf("tflite: op %d (%s) reads tensor %d before it is written", k, op.Code, i)
			}
		}
		run, err := it.compile(k, op)
		if err != nil {
			return nil, err
		}
		it.steps = append(it.steps, step{op: op.Code, idx: k, run: run})
		seen := map[int]bool{}
		for _, i := range op.Inputs {
			if i >= 0 && !isConst[i] && last[i] == s && !seen[i] {
				seen[i] = true
				free = append(free, it.data[i])
				sort.Slice(free, func(a, b int) bool { return cap(free[a]) < cap(free[b]) })
			}
		}
	}
	return it, nil
}

// Invoke runs the graph on the current input buffers.
func (it *Interpreter) Invoke() {
	for _, s := range it.steps {
		s.run()
	}
}

// Input returns input i's buffer (NHWC float32) to fill before Invoke.
func (it *Interpreter) Input(i int) []float32 { return it.data[it.m.Inputs[i]] }

// Output returns output i's buffer after Invoke.
func (it *Interpreter) Output(i int) []float32 { return it.data[it.m.Outputs[i]] }

// InputShape is input i's shape.
func (it *Interpreter) InputShape(i int) []int { return it.shapes[it.m.Inputs[i]] }

// OutputShape is output i's shape.
func (it *Interpreter) OutputShape(i int) []int { return it.shapes[it.m.Outputs[i]] }

// Tensor returns tensor i's data. With Options.KeepAll it holds the value
// the last Invoke computed; otherwise the buffer may since have been reused.
func (it *Interpreter) Tensor(i int) []float32 { return it.data[i] }

// Close stops the worker goroutines.
func (it *Interpreter) Close() { it.pool.close() }

func numElems(s []int) int {
	n := 1
	for _, d := range s {
		n *= d
	}
	return n
}

func decodeF32(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

func decodeF16(b []byte) []float32 {
	out := make([]float32, len(b)/2)
	for i := range out {
		out[i] = f16to32(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return out
}

// f16to32 converts an IEEE 754 binary16 to float32 exactly.
func f16to32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	man := uint32(h) & 0x3ff
	switch {
	case exp == 0 && man == 0:
		return math.Float32frombits(sign)
	case exp == 0: // subnormal: normalize
		e := uint32(127 - 15 + 1)
		for man&0x400 == 0 {
			man <<= 1
			e--
		}
		man &= 0x3ff
		return math.Float32frombits(sign | e<<23 | man<<13)
	case exp == 0x1f:
		return math.Float32frombits(sign | 0xff<<23 | man<<13)
	}
	return math.Float32frombits(sign | (exp+127-15)<<23 | man<<13)
}

// ints reads an INT32 constant tensor.
func (it *Interpreter) ints(i int) ([]int, error) {
	t := it.m.Tensors[i]
	if t.Type != Int32 || t.Data == nil {
		return nil, fmt.Errorf("tflite: tensor %d %q: want a constant INT32", i, t.Name)
	}
	out := make([]int, len(t.Data)/4)
	for k := range out {
		out[k] = int(int32(binary.LittleEndian.Uint32(t.Data[4*k:])))
	}
	return out, nil
}

var errUnsupported = errors.New("tflite: unsupported")
