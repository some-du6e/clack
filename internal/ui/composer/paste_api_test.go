package composer

import "encoding/json"

// The composer's paste hook into Slack's clipboard (hooks.go, taut.go),
// for paste_prepare_test.go.

func setPreparePaste(f func(kind string, src *Doc, formats map[string]any) *Doc) (restore func()) {
	prev := PreparePaste
	PreparePaste = f
	return func() { PreparePaste = prev }
}

func pasteHookOn() bool { return PreparePaste != nil }

func setTautKind(ed *Editor, kind string) { ed.TautKind = func() string { return kind } }

func focusComposer(kind string, on bool) {
	if ComposerFocus != nil {
		ComposerFocus(kind, on)
	}
}

func throughDelta(src *Doc, prepare func(ops json.RawMessage) (json.RawMessage, bool)) *Doc {
	return pasteThroughDelta(src, prepare)
}
