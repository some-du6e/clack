//go:build !js

package taut

// Taut's author credits, copied from its shared/authors.ts: real people's
// names, Slack ids and sites, so not in the browser demo, which has no
// plugins (authors_js.go).

// knownAuthors are the people Taut's plugins credit by key.
var knownAuthors = map[string]Author{
	"jeremy":  {"Jeremy", "U06UYA5GMB5", "https://jeremywoolley.com"},
	"rowan":   {"Rowan", "U080A3QP42C", "https://3kh0.net"},
	"ani":     {"Ani", "U01D9DWGEB0", "https://github.com/anirudhb"},
	"sahil":   {"Sahil", "U08PUHSMW4V", "https://sahil.ink"},
	"miggy":   {"Miggy", "U07VC9705D4", "https://shymike.dev"},
	"scooter": {"Scooter", "U046VA0KR8R", "https://github.com/scooterthedev"},
	"cyril":   {"Cyril", "U07FXPUDYDC", "https://github.com/CyrilSLi"},
	"izie":    {"Izie", "U09KKMHLS15", "https://izie.top"},
	"adryd":   {"adryd", "", "https://adryd.com"},
}
