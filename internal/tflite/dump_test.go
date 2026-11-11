package tflite

import (
	"fmt"
	"os"
	"sort"
	"testing"
)

// TestDump prints a model's operators: TFLITE_DUMP=path go test -run TestDump
func TestDump(t *testing.T) {
	path := os.Getenv("TFLITE_DUMP")
	if path == "" {
		t.Skip("TFLITE_DUMP not set")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("version %d %q, %d tensors, %d ops\n", m.Version, m.Description, len(m.Tensors), len(m.Ops))
	for _, i := range m.Inputs {
		fmt.Printf("input %d %s %v %s\n", i, m.Tensors[i].Name, m.Tensors[i].Shape, m.Tensors[i].Type)
	}
	for _, i := range m.Outputs {
		fmt.Printf("output %d %s %v %s\n", i, m.Tensors[i].Name, m.Tensors[i].Shape, m.Tensors[i].Type)
	}
	count := map[string]int{}
	types := map[string]int{}
	for _, t := range m.Tensors {
		if t.Data != nil {
			types[t.Type.String()]++
		}
	}
	var macs int64
	for k, op := range m.Ops {
		count[fmt.Sprintf("%s v%d %s", op.Code, op.Version, op.Custom)]++
		var shapes []string
		for _, i := range op.Inputs {
			if i < 0 {
				shapes = append(shapes, "-")
				continue
			}
			c := ""
			if m.Tensors[i].Data != nil {
				c = "c"
			}
			shapes = append(shapes, fmt.Sprintf("%v%s%s", m.Tensors[i].Shape, m.Tensors[i].Type.String()[:2], c))
		}
		out := m.Tensors[op.Outputs[0]].Shape
		n := int64(1)
		for _, d := range out {
			n *= int64(d)
		}
		switch op.Code {
		case OpConv2D:
			w := m.Tensors[op.Inputs[1]].Shape
			macs += n * int64(w[1]*w[2]*w[3])
		case OpDepthwiseConv2D:
			w := m.Tensors[op.Inputs[1]].Shape
			macs += n * int64(w[1]*w[2])
		case OpTransposeConv:
			w := m.Tensors[op.Inputs[1]].Shape
			in := m.Tensors[op.Inputs[2]].Shape
			macs += int64(in[1]*in[2]*in[3]) * int64(w[0]*w[1]*w[2])
		case OpFullyConnected:
			w := m.Tensors[op.Inputs[1]].Shape
			macs += n * int64(w[1])
		case OpBatchMatMul:
			a := m.Tensors[op.Inputs[0]].Shape
			macs += n * int64(a[len(a)-1])
		}
		if os.Getenv("TFLITE_DUMP_OPS") != "" {
			fmt.Printf("%3d %-18s %v -> %v opt%d %x", k, op.Code, shapes, out, op.optType, op.CustomOptions)
			if op.hasOpts {
				for f := 0; f < 7; f++ {
					if p := op.opts.field(f); p != 0 {
						fmt.Printf(" f%d=%d", f, op.opts.buf[p])
					}
				}
			}
			fmt.Println()
		}
	}
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%4d %s\n", count[k], k)
	}
	fmt.Printf("constant tensor types %v\nMACs %.1f M\n", types, float64(macs)/1e6)
	for k, v := range m.Metadata {
		fmt.Printf("metadata %s %d bytes\n", k, len(v))
	}
}
