package jsrt

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTautPlugins evaluates every plugin bundle built by taut's own
// `npm run build:user-plugin` (the jsrt workflow's taut-plugins job sets
// JSRT_TAUT_PLUGINS to the output directory) and checks that each yields a
// class extending TautPlugin, the way Taut's plugin manager loads them.
// TautAPI itself is internal/taut's; here only the engine and environment
// are under test.
func TestTautPlugins(t *testing.T) {
	dir := os.Getenv("JSRT_TAUT_PLUGINS")
	if dir == "" {
		t.Skip("JSRT_TAUT_PLUGINS not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.js"))
	if len(files) == 0 {
		t.Fatalf("no plugins in %s", dir)
	}
	r := newTest(t)
	err := r.Do(func(js *Ctx) error {
		_, err := js.Eval("taut-stub.js", `
globalThis.TautPlugin = class TautPlugin { constructor(api, config) { this.api = api; this.config = config } start() {} stop() {} };
const anyOpt = () => new Proxy(function () {}, { get: (t, k) => k === Symbol.toPrimitive ? () => '' : anyOpt(), apply: () => anyOpt() });
globalThis.TautOpt = anyOpt();
globalThis.__load = (code) => {
  const result = new Function('return ' + code)();
  const C = result && result.prototype instanceof TautPlugin ? result : result && result.default;
  if (!C || !(C.prototype instanceof TautPlugin)) throw new Error('plugin class does not extend TautPlugin');
  return { id: C.id ?? C.name, name: C.pluginName ?? C.name };
};`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(f)
		err = r.Do(func(js *Ctx) error {
			v, err := js.Global().Get("__load").Call(string(src))
			if err != nil {
				return err
			}
			b, _ := v.JSON()
			t.Logf("%s: %s", name, b)
			return nil
		})
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
