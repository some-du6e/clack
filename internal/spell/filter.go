package spell

import (
	"encoding/base64"
	"strings"
	"sync"
)

// The desktop app drops offensive suggestions from its context menu
// (main.bundle.cjs addSpellingItems: suggestions t with mu(t), or any
// piece of t split at /[ -]/ with mu, are skipped; mu tests the mf set,
// which Slack ships as byte arrays). The same 71 words, base64-encoded
// one per line so the source doesn't spell them out.
const blockedB64 = "YXNzLXBpcmF0ZQphc3NiYW5kaXQKYXNzYmFuZ2VyCmFzc2Z1Y2tlcgphc3Nnb2JsaW4KYXNzaG9wcGVyCmFzc2phY2tlcgphc3NuaWdnZXIKYXNzcGlyYXRlCmJlYW5lcgpiaXRjaHRpdApjb2Nrc3Vja2VyCmNvY2tzdWNraW5nCmNvY2tzdWNrcwpjdW50bGlja2VyCmRhZ28KZGVnZ28KZG91Y2hld2FmZmxlCmZhZwpmYWdiYWcKZmFnZnVja2VyCmZhZ2dpbmcKZmFnZ2l0CmZhZ2dvdApmYWdnb3RzCmZhZ2dvdGNvY2sKZmFnZ3MKZmFnb3QKZmFncwpmYWd0YXJkCmZsYW1lcgpmdWRnZXBhY2tlcgpnb29rCmdyaW5nbwpndWlkbwpoZWViCmhvCmhvZQpob21vCmhvbmtleQpqYXAKamlnYWJvbwpqdW5nbGUgYnVubnkKanVuZ2xlYnVubnkKa2lrZQpreWtlCmxlenppZQptY2ZhZ2dldAptaWNrCm11ZmZkaXZlcgpuZWdybwpuaWdnYQpuaWdnZXIKbmlnbGV0CnBha2kKcGVuaXNmdWNrZXIKcGVuaXNwdWZmZXIKcG9sZXNtb2tlcgpwb2xsb2NrCnBvcmNoIG1vbmtleQpwb3JjaG1vbmtleQpydXNraQpzYW5kbmlnZ2VyCnNsdXQKc2x1dGJhZwpzcGljCnNwaWNrCnRhcmQKdHdhdHdhZmZsZQp1bmNsZWZ1Y2tlcgp3ZXRiYWNr"

var (
	blockedOnce sync.Once
	blocked     map[string]bool
)

func isBlocked(w string) bool {
	blockedOnce.Do(func() {
		blocked = map[string]bool{}
		b, _ := base64.StdEncoding.DecodeString(blockedB64)
		for _, x := range strings.Split(string(b), "\n") {
			blocked[x] = true
		}
	})
	return blocked[w]
}

// FilterSuggestions is addSpellingItems' filter: the exact suggestion or
// any of its space- or hyphen-separated pieces on the list removes it.
func FilterSuggestions(in []string) []string {
	var out []string
	for _, s := range in {
		if isBlocked(s) {
			continue
		}
		bad := false
		for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '-' }) {
			if isBlocked(p) {
				bad = true
				break
			}
		}
		if !bad {
			out = append(out, s)
		}
	}
	return out
}
