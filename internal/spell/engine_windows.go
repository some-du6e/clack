//go:build windows

package spell

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows: Chromium's hybrid checker (components/spellcheck/browser/
// windows_spell_checker.cc). A language the Windows spell checker
// supports (ISpellCheckerFactory::IsSupported) is checked by it; the rest
// use Hunspell dictionaries. A word is misspelled when ComprehensiveCheck
// reports an error whose corrective action is GET_SUGGESTIONS or REPLACE
// (not DELETE, the repeated-word action); Suggest fills the menu and Add
// puts the word in the user's Windows dictionary.

var (
	clsidSpellCheckerFactory = windows.GUID{Data1: 0x7AB36653, Data2: 0x1796, Data3: 0x484B, Data4: [8]byte{0xBD, 0xFA, 0xE7, 0x4F, 0x1D, 0xB7, 0xC1, 0xDC}}
	iidISpellCheckerFactory  = windows.GUID{Data1: 0x8E018A9D, Data2: 0x2415, Data3: 0x4677, Data4: [8]byte{0xBF, 0x08, 0x79, 0x4E, 0xA6, 0x1F, 0x94, 0xBB}}

	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")

	kernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	procGetUserDefaultLocaleNam = kernel32.NewProc("GetUserDefaultLocaleName")
)

const (
	clsctxInprocServer = 0x1
	clsctxLocalServer  = 0x4

	actionGetSuggestions = 1
	actionReplace        = 2
)

// comObj is a COM interface pointer: its first word is the vtable.
type comObj struct{ vtbl *[32]uintptr }

func (o *comObj) call(i int, args ...uintptr) uintptr {
	a := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	r, _, _ := syscall.SyscallN(o.vtbl[i], a...)
	return r
}

func (o *comObj) release() {
	if o != nil {
		o.call(2)
	}
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

var factory *comObj // worker thread only

func platformThreadInit() {
	// STA, as Chromium's background helper asserts
	_ = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	var f *comObj
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidSpellCheckerFactory)), 0,
		clsctxInprocServer|clsctxLocalServer, uintptr(unsafe.Pointer(&iidISpellCheckerFactory)), uintptr(unsafe.Pointer(&f)))
	if !failed(hr) && f != nil {
		factory = f
	}
}

type winEngine struct{ c *comObj }

// ISpellChecker vtable: 0-2 IUnknown, 3 get_LanguageTag, 4 Check,
// 5 Suggest, 6 Add, 7 Ignore, 8 AutoCorrect, 9 GetOptionValue,
// 10 get_OptionIds, 11 get_Id, 12 get_LocalizedName,
// 13 add_SpellCheckerChanged, 14 remove_SpellCheckerChanged,
// 15 GetOptionDescription, 16 ComprehensiveCheck.

func (e winEngine) Check(w string) bool {
	p, err := windows.UTF16PtrFromString(w)
	if err != nil {
		return true
	}
	var errs *comObj
	if hr := e.c.call(16, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&errs))); failed(hr) || errs == nil {
		return true
	}
	defer errs.release()
	for {
		var se *comObj
		// IEnumSpellingError::Next: S_OK with an error, S_FALSE at the end
		if hr := errs.call(3, uintptr(unsafe.Pointer(&se))); hr != 0 || se == nil {
			return true
		}
		var action uint32
		// ISpellingError: 3 get_StartIndex, 4 get_Length, 5 get_CorrectiveAction
		hr := se.call(5, uintptr(unsafe.Pointer(&action)))
		se.release()
		if !failed(hr) && (action == actionGetSuggestions || action == actionReplace) {
			return false
		}
	}
}

func (e winEngine) Suggest(w string) []string {
	p, err := windows.UTF16PtrFromString(w)
	if err != nil {
		return nil
	}
	var en *comObj
	if hr := e.c.call(5, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&en))); failed(hr) || en == nil {
		return nil
	}
	defer en.release()
	var out []string
	for len(out) < MaxSuggestions {
		var s *uint16
		var got uint32
		// IEnumString::Next(1, &s, &fetched)
		if hr := en.call(3, 1, uintptr(unsafe.Pointer(&s)), uintptr(unsafe.Pointer(&got))); hr != 0 || s == nil {
			break
		}
		out = append(out, windows.UTF16PtrToString(s))
		procCoTaskMemFree.Call(uintptr(unsafe.Pointer(s)))
	}
	return out
}

func (e winEngine) Add(w string) {
	if p, err := windows.UTF16PtrFromString(w); err == nil {
		e.c.call(6, uintptr(unsafe.Pointer(p)))
	}
}

func (e winEngine) Close() { e.c.release() }

// openWindows returns the Windows checker for lang, if it supports it.
func openWindows(lang string) (Engine, bool) {
	if factory == nil {
		return nil, false
	}
	tag, err := windows.UTF16PtrFromString(lang)
	if err != nil {
		return nil, false
	}
	// ISpellCheckerFactory: 3 get_SupportedLanguages, 4 IsSupported,
	// 5 CreateSpellChecker
	var ok int32
	if hr := factory.call(4, uintptr(unsafe.Pointer(tag)), uintptr(unsafe.Pointer(&ok))); failed(hr) || ok == 0 {
		return nil, false
	}
	var c *comObj
	if hr := factory.call(5, uintptr(unsafe.Pointer(tag)), uintptr(unsafe.Pointer(&c))); failed(hr) || c == nil {
		return nil, false
	}
	return winEngine{c}, true
}

// WindowsSupports reports whether the Windows spell checker handles lang
// (tests; call on the worker's thread setup only in tests).
func WindowsSupports(lang string) bool {
	e, ok := openWindows(lang)
	if ok {
		e.Close()
	}
	return ok
}

func platformOpen(langs []string, dir string) ([]Engine, error) {
	var out []Engine
	var errs []error
	for _, l := range langs {
		if e, ok := openWindows(l); ok {
			out = append(out, e)
			continue
		}
		e, err := openHunspell(l, dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, e)
	}
	return out, errors.Join(errs...)
}

func platformLocale() string {
	buf := make([]uint16, 85)
	r, _, _ := procGetUserDefaultLocaleNam.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// SystemLanguages: Preferences lists the languages on Windows.
const SystemLanguages = false
