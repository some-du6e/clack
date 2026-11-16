package gif

// The vendored decoder decodes exactly as Go's own (image/gif) does:
// every file of the Go toolchain's image test corpus (runtime.GOROOT()'s
// src/image/testdata and src/image/gif/testdata) is decoded both ways and
// compared pixel by pixel (the Clack changes charge, and give back, memory;
// they mustn't change a pixel). Skipped where the toolchain's sources
// aren't there.

import (
	"bytes"
	"image"
	stdlib "image/gif"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
)

func corpus(t *testing.T, ext string) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{"src/image/testdata", "src/image/gif/testdata"} {
		matches, _ := filepath.Glob(filepath.Join(runtime.GOROOT(), dir, "*"+ext))
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Skipf("no %s files under GOROOT %s", ext, runtime.GOROOT())
	}
	return files
}

func sameImage(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				return false
			}
		}
	}
	return true
}

func TestDecodesAsStdlib(t *testing.T) {
	n := 0
	for _, f := range corpus(t, ".gif") {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		want, werr := stdlib.Decode(bytes.NewReader(data))
		b := budget.NewLocal(1 << 30)
		got, gerr := Decode(bytes.NewReader(data), b)
		name := filepath.Base(f)
		if (werr == nil) != (gerr == nil) {
			if strings.HasPrefix(name, "invalid") && werr != nil && gerr != nil {
				continue
			}
			t.Errorf("%s: stdlib %v, vendored %v", name, werr, gerr)
			continue
		}
		if werr != nil {
			continue
		}
		if !sameImage(got, want) {
			t.Errorf("%s: the vendored decode differs from stdlib's", name)
		}
		n++
	}
	if n == 0 {
		t.Error("no corpus file decoded")
	}
	t.Logf("%d files decoded identically", n)
}
