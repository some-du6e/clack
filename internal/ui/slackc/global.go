package slackc

import (
	"strconv"
	"strings"

	"gioui.org/io/key"
)

// Window/document-level input for plugins that listen there (Oneko's
// document mousemove, StreamerMode's window keydown capture). The bridge
// may implement WantsEvent; when it doesn't, or no listener exists,
// nothing is forwarded.

type globalInput interface{ WantsEvent(typ string) bool }

// WantsGlobal reports whether JS has a window/document listener for the
// DOM event type. Cheap (no JS round trip); false without a runtime.
func WantsGlobal(typ string) bool {
	b := bridge()
	if b == nil {
		return false
	}
	g, ok := b.(globalInput)
	return ok && g.WantsEvent(typ)
}

// GlobalPointer forwards a window pointer event (window px; scale is
// px per CSS px) to JS at target 0.
func GlobalPointer(typ string, x, y, scale float32, buttons int) {
	b := bridge()
	if b == nil {
		return
	}
	if scale <= 0 {
		scale = 1
	}
	ev := make([]byte, 0, 96)
	ev = append(ev, `{"type":`...)
	ev = appendString(ev, typ)
	ev = append(ev, `,"clientX":`...)
	ev = strconv.AppendFloat(ev, float64(x/scale), 'f', 1, 32)
	ev = append(ev, `,"clientY":`...)
	ev = strconv.AppendFloat(ev, float64(y/scale), 'f', 1, 32)
	ev = append(ev, `,"buttons":`...)
	ev = strconv.AppendInt(ev, int64(buttons), 10)
	ev = append(ev, '}')
	b.Dispatch(0, ev)
}

// GlobalKey forwards a key event ("keydown"/"keyup") to JS at target 0;
// JS routes it to its focused node or the document.
func GlobalKey(typ string, e key.Event) {
	b := bridge()
	if b == nil {
		return
	}
	b.Dispatch(0, KeyEventJSON(typ, e))
}

// KeyEventJSON encodes a Gio key event as a DOM KeyboardEvent init.
func KeyEventJSON(typ string, e key.Event) []byte {
	k, code := DOMKey(e.Name, e.Modifiers)
	ev := make([]byte, 0, 128)
	ev = append(ev, `{"type":`...)
	ev = appendString(ev, typ)
	ev = append(ev, `,"key":`...)
	ev = appendString(ev, k)
	ev = append(ev, `,"code":`...)
	ev = appendString(ev, code)
	m := e.Modifiers
	ev = append(ev, `,"ctrlKey":`...)
	ev = strconv.AppendBool(ev, m.Contain(key.ModCtrl))
	ev = append(ev, `,"shiftKey":`...)
	ev = strconv.AppendBool(ev, m.Contain(key.ModShift))
	ev = append(ev, `,"altKey":`...)
	ev = strconv.AppendBool(ev, m.Contain(key.ModAlt))
	ev = append(ev, `,"metaKey":`...)
	ev = strconv.AppendBool(ev, m.Contain(key.ModSuper) || m.Contain(key.ModCommand))
	return append(ev, '}')
}

var domKeys = map[key.Name][2]string{
	key.NameReturn: {"Enter", "Enter"}, key.NameEnter: {"Enter", "NumpadEnter"},
	key.NameEscape: {"Escape", "Escape"}, key.NameTab: {"Tab", "Tab"}, key.NameSpace: {" ", "Space"},
	key.NameDeleteBackward: {"Backspace", "Backspace"}, key.NameDeleteForward: {"Delete", "Delete"},
	key.NameLeftArrow: {"ArrowLeft", "ArrowLeft"}, key.NameRightArrow: {"ArrowRight", "ArrowRight"},
	key.NameUpArrow: {"ArrowUp", "ArrowUp"}, key.NameDownArrow: {"ArrowDown", "ArrowDown"},
	key.NameHome: {"Home", "Home"}, key.NameEnd: {"End", "End"},
	key.NamePageUp: {"PageUp", "PageUp"}, key.NamePageDown: {"PageDown", "PageDown"},
	key.NameCtrl: {"Control", "ControlLeft"}, key.NameShift: {"Shift", "ShiftLeft"},
	key.NameAlt: {"Alt", "AltLeft"}, key.NameSuper: {"Meta", "MetaLeft"}, key.NameCommand: {"Meta", "MetaLeft"},
	key.NameF1: {"F1", "F1"}, key.NameF2: {"F2", "F2"}, key.NameF3: {"F3", "F3"}, key.NameF4: {"F4", "F4"},
	key.NameF5: {"F5", "F5"}, key.NameF6: {"F6", "F6"}, key.NameF7: {"F7", "F7"}, key.NameF8: {"F8", "F8"},
	key.NameF9: {"F9", "F9"}, key.NameF10: {"F10", "F10"}, key.NameF11: {"F11", "F11"}, key.NameF12: {"F12", "F12"},
}

var punct = map[string][2]string{
	",": {",", "Comma"}, ".": {".", "Period"}, "/": {"/", "Slash"}, ";": {";", "Semicolon"},
	"'": {"'", "Quote"}, "[": {"[", "BracketLeft"}, "]": {"]", "BracketRight"}, "\\": {"\\", "Backslash"},
	"-": {"-", "Minus"}, "=": {"=", "Equal"}, "`": {"`", "Backquote"},
}

// DOMKey maps a Gio key name to DOM KeyboardEvent key and code.
func DOMKey(n key.Name, m key.Modifiers) (string, string) {
	if v, ok := domKeys[n]; ok {
		return v[0], v[1]
	}
	s := string(n)
	if len(s) == 1 {
		c := s[0]
		switch {
		case c >= 'A' && c <= 'Z':
			if m.Contain(key.ModShift) {
				return s, "Key" + s
			}
			return strings.ToLower(s), "Key" + s
		case c >= '0' && c <= '9':
			return s, "Digit" + s
		}
		if v, ok := punct[s]; ok {
			return v[0], v[1]
		}
	}
	return s, s
}
