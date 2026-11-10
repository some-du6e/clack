package tflite

import "sync"

// workers splits a loop over rows between goroutines.
type workers struct{ n int }

func newWorkers(n int) *workers { return &workers{n: max(1, n)} }

func (w *workers) close() {}

// minWork is the multiply-adds below which a kernel stays on one goroutine:
// starting goroutines costs more than it saves for the small layers.
const minWork = 200_000

// rows runs f over [0, n) in contiguous chunks, on up to w.n goroutines
// when work (the kernel's multiply-adds) is big enough to be worth it.
func (w *workers) rows(n int, work int, f func(lo, hi int)) {
	parts := w.n
	if work < minWork || n < 2 || parts == 1 {
		f(0, n)
		return
	}
	parts = min(parts, n)
	var wg sync.WaitGroup
	wg.Add(parts - 1)
	for p := 1; p < parts; p++ {
		lo, hi := n*p/parts, n*(p+1)/parts
		go func() {
			defer wg.Done()
			f(lo, hi)
		}()
	}
	f(0, n/parts)
	wg.Wait()
}
