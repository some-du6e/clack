// Engine comparison (QuickJS-NG on wazero vs goja) for internal/jsrt.
// A separate module so goja never becomes a Clack dependency. Run in CI by
// .github/workflows/jsrt.yml (workflow_dispatch, bench=true).
module github.com/some-du6e/clack/internal/jsrt/enginecmp

go 1.26.0

require (
	github.com/dop251/goja v0.0.0-20261107161106-d6eaa50db8bc
	github.com/some-du6e/clack v0.0.0
)

require (
	github.com/dlclark/regexp2/v2 v2.11.0 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	github.com/tetratelabs/wazero v1.11.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/some-du6e/clack => ../../..
