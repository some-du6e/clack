//go:build js

package jsrt

// The browser demo (cmd/clack-web) leaves plugins' JavaScript runtime out:
// QuickJS on wazero (engine.go, wasmmem.go and the jsrt.wasm they load)
// isn't in its build. newEngine fails, so New returns errNoEngine and no
// Runtime is ever made. The methods below are the ones the rest of the
// package calls, with engine.go's signatures, so it compiles; nothing
// calls them. When engine.go's methods change, these follow.

import "errors"

var errNoEngine = errors.New("plugins' JavaScript runtime isn't part of the browser demo")

// engineWasm is engine.go's compressed jsrt.wasm: none in the browser.
var engineWasm = func() []byte { return nil }

type engine struct {
	r     *Runtime
	mod   any
	fns   map[string]any
	dirty bool
}

func newEngine(r *Runtime) (*engine, error) { return nil, errNoEngine }

func (e *engine) close()                                      {}
func (e *engine) call(name string, args ...uint64) uint64     { return *new(uint64) }
func (e *engine) tmpBytes(b []byte) (ptr uint32, free func()) { return *new(uint32), *new(func()) }
func (e *engine) read(ptr, n uint32) []byte                   { return *new([]byte) }
func (e *engine) res(h uint32) (uint32, error)                { return *new(uint32), errNoEngine }
func (e *engine) takeError(h uint32) error                    { return errNoEngine }
func (e *engine) toError(h uint32) error                      { return errNoEngine }
func (e *engine) release(h uint32)                            {}
func (e *engine) typeOf(h uint32) string                      { return *new(string) }
func (e *engine) is(h uint32, what int) bool                  { return *new(bool) }
func (e *engine) truthy(h uint32) bool                        { return *new(bool) }
func (e *engine) toFloat(h uint32) float64                    { return *new(float64) }
func (e *engine) toString(h uint32) string                    { return *new(string) }
func (e *engine) global() uint32                              { return *new(uint32) }
func (e *engine) null() uint32                                { return *new(uint32) }
func (e *engine) newBool(b bool) uint32                       { return *new(uint32) }
func (e *engine) newNumber(f float64) uint32                  { return *new(uint32) }
func (e *engine) newString(s string) uint32                   { return *new(uint32) }
func (e *engine) newObject() uint32                           { return *new(uint32) }
func (e *engine) newArray() uint32                            { return *new(uint32) }
func (e *engine) newBytes(b []byte) uint32                    { return *new(uint32) }
func (e *engine) parse(b []byte) (uint32, error)              { return *new(uint32), errNoEngine }
func (e *engine) newError(err error) uint32                   { return *new(uint32) }
func (e *engine) get(o uint32, k string) (uint32, error)      { return *new(uint32), errNoEngine }
func (e *engine) getIndex(o, i uint32) (uint32, error)        { return *new(uint32), errNoEngine }
func (e *engine) set(o uint32, k string, v uint32) error      { return errNoEngine }
func (e *engine) setIndex(o, i, v uint32) error               { return errNoEngine }
func (e *engine) del(o uint32, k string) error                { return errNoEngine }
func (e *engine) has(o uint32, k string) bool                 { return *new(bool) }
func (e *engine) keys(o uint32) []string                      { return *new([]string) }
func (e *engine) callFn(fn, this uint32, args []uint32) (uint32, error) {
	return *new(uint32), errNoEngine
}
func (e *engine) construct(fn uint32, args []uint32) (uint32, error) {
	return *new(uint32), errNoEngine
}
func (e *engine) strictEq(a, b uint32) bool          { return *new(bool) }
func (e *engine) stringify(h uint32) ([]byte, error) { return *new([]byte), errNoEngine }
func (e *engine) bytes(h uint32) []byte              { return *new([]byte) }
func (e *engine) newPromise() (p, resolve, reject uint32) {
	return *new(uint32), *new(uint32), *new(uint32)
}
func (e *engine) newFunc(id uint32, name string) uint32 { return *new(uint32) }
func (e *engine) eval(name, src string, module bool) (uint32, error) {
	return *new(uint32), errNoEngine
}
func (e *engine) runJobs(js *Ctx)                                   {}
func (e *engine) hasDirty() bool                                    { return *new(bool) }
func (e *engine) gcFloor()                                          {}
func (e *engine) exposeHost(name string, id uint32)                 {}
func (e *engine) importModule(js *Ctx, name string) (uint32, error) { return *new(uint32), errNoEngine }
func (e *engine) fireTimer(js *Ctx, id int)                         {}
