package jsrt

// Vendored React (see vendor/README.md), from assets.pack. Each CommonJS
// file is wrapped in a factory registered with env.js's require(). Built per
// boot: as a package-level var the wrapping cost 1.8 MB of heap in every
// process, plugins or not.

type script struct{ name, src string }

func cjs(name, src string) script {
	return script{name, "__jr.define(" + quoteJS(name) + ", function (module, exports, require) {\n" + src + "\n});"}
}

func quoteJS(s string) string {
	b, _ := jsonMarshal(s)
	return string(b)
}

func vendorScripts(f map[string][]byte) []script {
	v := func(name string) string { return string(f["vendor/"+name+".production.js"]) }
	return []script{
		cjs("react", v("react")),
		cjs("react/jsx-runtime", v("react-jsx-runtime")),
		cjs("scheduler", v("scheduler")),
		cjs("react-reconciler", v("react-reconciler")),
		cjs("react-reconciler/constants", v("react-reconciler-constants")),
	}
}
