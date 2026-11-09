package jsrt

import (
	"runtime"
	"testing"
	"time"
)

// TestFreedValueNotReleasedTwice: a Value freed by hand (NewPromise's
// settle functions, after every resolve) must not be released again when
// Go collects it. The engine reuses the freed handle number for the next
// value, so a second release frees that live value's slot, and the slot
// then holds whatever comes after: Taut's componentElement saw its props
// or its element turn into __clack (React #31) and its function into a
// non-function ("TypeError: not a function").
func TestFreedValueNotReleasedTwice(t *testing.T) {
	r := newTest(t)
	var live Value
	func() {
		var resolve func(any)
		if err := r.Do(func(js *Ctx) error {
			_, resolve, _ = js.NewPromise()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		resolve(1) // Posts the settle, which Frees resolve's and reject's Values
		if err := r.Do(func(js *Ctx) error {
			// takes the handle number reject's Value just gave back
			var err error
			live, err = js.ParseJSON([]byte(`{"keep":"me"}`))
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}()
	// collect the freed Values (resolve is out of scope) and let any
	// cleanup's release run on the JS thread; then hand out more handles
	for i := 0; i < 20; i++ {
		runtime.GC()
		time.Sleep(5 * time.Millisecond)
		_ = r.Do(func(js *Ctx) error {
			for j := 0; j < 4; j++ {
				_, _ = js.ParseJSON([]byte(`"other"`))
			}
			return nil
		})
	}
	var got string
	if err := r.Do(func(js *Ctx) error {
		got = live.Get("keep").String()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got != "me" {
		t.Fatalf(`live value's "keep" is %q after the freed Values were collected; its handle was released twice`, got)
	}
}
