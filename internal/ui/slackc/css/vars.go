package css

import "sync"

var (
	slackVarsOnce               sync.Once
	slackLightRes, slackDarkRes map[string]string
)

// SlackVars are Slack's design-token custom properties (--sk_*, --dt_*,
// fonts, …) as defined at :root/body/.p-client_container and by the
// .sk-client-theme--dark rules, captured from the live web client (see
// slackvars.go). var() references inside them are already substituted.
// The returned map is shared: never modify it.
func SlackVars(dark bool) map[string]string {
	slackVarsOnce.Do(func() {
		slackLightRes = resolveVarMap(slackVarsLight)
		slackDarkRes = resolveVarMap(slackVarsDark)
	})
	if dark {
		return slackDarkRes
	}
	return slackLightRes
}

// resolveVarMap substitutes var() inside a root custom property set.
func resolveVarMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	var look func(name string, depth int) (string, bool)
	look = func(name string, depth int) (string, bool) {
		v, ok := m[name]
		if !ok || depth > 24 {
			return "", false
		}
		if !hasVar(v) {
			return v, true
		}
		return substVars(v, func(n string) (string, bool) { return look(n, depth+1) }, depth+1)
	}
	for k := range m {
		if v, ok := look(k, 0); ok {
			out[k] = v
		}
	}
	return out
}
