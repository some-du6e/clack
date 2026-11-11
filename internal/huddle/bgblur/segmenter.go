package bgblur

import (
	"fmt"
	"sync"

	"github.com/some-du6e/clack/internal/tflite"
)

var (
	modelOnce sync.Once
	parsed    *tflite.Model
	parseErr  error
)

func loadModel() (*tflite.Model, error) {
	modelOnce.Do(func() { parsed, parseErr = tflite.Parse(ModelTFLite) })
	return parsed, parseErr
}

// Segmenter runs the selfie segmenter: an RGB picture of W×H in [0, 1]
// to the probability that each pixel is a person.
type Segmenter struct {
	it   *tflite.Interpreter
	W, H int
}

// NewSegmenter loads the embedded model; threads as tflite.Options.
func NewSegmenter(threads int) (*Segmenter, error) {
	m, err := loadModel()
	if err != nil {
		return nil, err
	}
	it, err := tflite.NewInterpreter(m, tflite.Options{Threads: threads})
	if err != nil {
		return nil, err
	}
	s := it.InputShape(0)
	o := it.OutputShape(0)
	if len(s) != 4 || s[3] != 3 || len(o) != 4 || o[1] != s[1] || o[2] != s[2] || o[3] != 1 {
		it.Close()
		return nil, fmt.Errorf("bgblur: unexpected model shapes %v → %v", s, o)
	}
	return &Segmenter{it: it, W: s[2], H: s[1]}, nil
}

// Input is the H×W×3 RGB buffer to fill before Run.
func (s *Segmenter) Input() []float32 { return s.it.Input(0) }

// Run segments Input and returns the person probability, H×W. The slice
// is reused by the next Run.
func (s *Segmenter) Run() []float32 {
	s.it.Invoke()
	return s.it.Output(0)
}

// Close releases the interpreter.
func (s *Segmenter) Close() { s.it.Close() }
