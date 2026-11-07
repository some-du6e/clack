package jsrt

import _ "embed"

// Vendored React (see vendor/README.md). Each CommonJS file is wrapped in a
// factory registered with env.js's require().
var (
	//go:embed vendor/react.production.js
	vReact string
	//go:embed vendor/react-jsx-runtime.production.js
	vJSX string
	//go:embed vendor/scheduler.production.js
	vScheduler string
	//go:embed vendor/react-reconciler.production.js
	vReconciler string
	//go:embed vendor/react-reconciler-constants.production.js
	vReconcilerConstants string
)

type script struct{ name, src string }

func cjs(name, src string) script {
	return script{name, "__jr.define(" + quoteJS(name) + ", function (module, exports, require) {\n" + src + "\n});"}
}

func quoteJS(s string) string {
	b, _ := jsonMarshal(s)
	return string(b)
}

var vendorScripts = []script{
	cjs("react", vReact),
	cjs("react/jsx-runtime", vJSX),
	cjs("scheduler", vScheduler),
	cjs("react-reconciler", vReconciler),
	cjs("react-reconciler/constants", vReconcilerConstants),
}
