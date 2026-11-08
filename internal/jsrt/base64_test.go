package jsrt

import (
	"strings"
	"testing"
	"time"
)

func TestAtobBtoaGo(t *testing.T) {
	for in, want := range map[string]string{"aGk=": "hi", "aGk": "hi", " aG\nk= ": "hi", "": "", "/w==": "ÿ", "AAEC": "\x00\x01\x02"} {
		if got, ok := atobGo(in); !ok || got != want {
			t.Errorf("atob(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"a", "a===", "ab=c", "*abc", "aGk==="} {
		if _, ok := atobGo(bad); ok {
			t.Errorf("atob(%q) accepted", bad)
		}
	}
	for in, want := range map[string]string{"hi": "aGk=", "ÿ": "/w==", "": ""} {
		if got, ok := btoaGo(in); !ok || got != want {
			t.Errorf("btoa(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := btoaGo("π"); ok {
		t.Error("btoa accepted a code point above U+00FF")
	}
	// fast: a dictionary-sized payload in well under the task timeout
	big := strings.Repeat("éabc", 200000)
	t0 := time.Now()
	enc, ok := btoaGo(big)
	dec, ok2 := atobGo(enc)
	if !ok || !ok2 || dec != big || time.Since(t0) > 2*time.Second {
		t.Errorf("round trip ok=%v/%v equal=%v in %v", ok, ok2, dec == big, time.Since(t0))
	}
}
