//go:build darwin && cgo

package spell

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include <stdlib.h>

int clackSpellReady(void);
int clackSpellCheck(const char *word);
char *clackSpellGuesses(const char *word);
void clackSpellLearn(const char *word);
long clackSpellTag(void);
char *clackSpellLocale(void);
*/
import "C"

import (
	"strings"
	"unsafe"
)

// macOS: the system spell checker (NSSpellChecker), as Chromium's
// spellcheck_platform_mac.mm uses it: checkSpellingOfString with language
// nil (the system's automatic language detection), guessesForWordRange in
// the checker's language, learnWord for Add to Dictionary. The languages
// are the system's (System Settings > Keyboard > Text), so Preferences
// shows Slack's hint instead of a list. One engine stands for them all.

type macEngine struct{}

func (macEngine) Check(w string) bool {
	cs := C.CString(w)
	defer C.free(unsafe.Pointer(cs))
	return C.clackSpellCheck(cs) != 0
}

func (macEngine) Suggest(w string) []string {
	cs := C.CString(w)
	defer C.free(unsafe.Pointer(cs))
	r := C.clackSpellGuesses(cs)
	if r == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(r))
	s := C.GoString(r)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func (macEngine) Add(w string) {
	cs := C.CString(w)
	defer C.free(unsafe.Pointer(cs))
	C.clackSpellLearn(cs)
}

func (macEngine) Close() {}

func platformOpen(langs []string, dir string) ([]Engine, error) {
	if C.clackSpellReady() == 0 {
		return openHunspellAll(langs, dir) // no AppKit spell server: Hunspell
	}
	return []Engine{macEngine{}}, nil
}

func platformThreadInit() {}

func platformLocale() string {
	r := C.clackSpellLocale()
	if r == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(r))
	return C.GoString(r)
}

// SystemLanguages: on macOS the system picks the languages.
const SystemLanguages = true
