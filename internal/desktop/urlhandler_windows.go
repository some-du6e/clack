package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Clack as a slack:// handler, for the current user only (HKCU, no admin
// rights):
//
//	Software\Classes\Clack.slack            the ProgID: (Default) "URL:slack Protocol",
//	                                         URL Protocol "", DefaultIcon,
//	                                         shell\open\command "Clack.exe" "%1"
//	Software\Clack\Capabilities             ApplicationName Clack, ApplicationDescription
//	  URLAssociations                       slack = Clack.slack
//	Software\RegisteredApplications         Clack = Software\Clack\Capabilities
//	Software\Classes\slack\OpenWithProgids  Clack.slack (REG_NONE), only in a slack key
//	                                         that is a registration already
//
// That is Default Programs' registration (Microsoft Learn, "Default
// Programs": an application-specific ProgID per protocol, a Capabilities
// key from HKCU or HKLM whose ApplicationDescription is required and whose
// ApplicationName matches the RegisteredApplications value), and Slick's
// (install.ps1's Register-SlackHandler writes the same keys as Slick.slack
// and Software\Slick, OpenWithProgids included, and Slick is listed in
// Windows' box). Windows builds its "Select an app to open this 'slack'
// link" box and Settings › Default apps from registered applications, so
// Clack is offered there next to Slack desktop instead of fighting it for
// one key.
//
// The legacy key, Software\Classes\slack (Microsoft Learn, "Registering an
// Application to a URI Scheme"), is what runs when Windows doesn't ask.
// Slack desktop writes it at every start (Electron's
// setAsDefaultProtocolClient) and Slick at install. Clack takes it only
// when no other app opens slack:// (Default Programs: "An application
// should never reclaim a default without asking the user"), or when asked
// to (-register-url-handler, the installer's box). The user's own choice,
// …\Shell\Associations\UrlAssociations\slack\UserChoice, is hash-protected
// and Clack never writes it: Windows does, when the user picks Clack and
// Always.

const (
	urlProgID  = "Clack.slack"
	urlAppName = "Clack" // the RegisteredApplications value; Capabilities' ApplicationName matches it
	urlAppDesc = "Clack is a native Slack client. It signs in with Slack's browser sign-in and opens slack:// links."
)

// urlKeys is where the registration lives under HKCU (tests move it under
// a scratch key) and how it asks Windows about other apps.
type urlKeys struct {
	classes  string        // Software\Classes
	software string        // Software
	owner    func() string // another app that opens slack:// now, or ""
	notify   func()        // tell the shell associations changed
}

var userURLKeys = urlKeys{classes: `Software\Classes`, software: `Software`, owner: shellOwner, notify: assocChanged}

func (r urlKeys) progID() string { return r.classes + `\` + urlProgID }
func (r urlKeys) legacy() string { return r.classes + `\slack` }
func (r urlKeys) caps() string   { return r.software + `\Clack\Capabilities` }
func (r urlKeys) apps() string   { return r.software + `\RegisteredApplications` }

// urlMu keeps the start's registration and a later one from interleaving.
var urlMu sync.Mutex

// URLHandlerAtStart reports whether this copy registers itself as a
// slack:// handler when it starts: every Clack.exe, installed or
// portable, except a `go run` or `go test` build (its executable is a
// temporary file in a go-build directory).
func URLHandlerAtStart() bool {
	exe, err := os.Executable()
	return err == nil && !strings.Contains(strings.ToLower(exe), `\go-build`)
}

// EnsureURLHandler is the registration at every start: Clack's own keys,
// always, with this executable's path (so an update or a moved portable
// copy stays current), and the legacy key only when no other app opens
// slack://. It writes only what differs, so a start that finds everything
// in place only reads.
func EnsureURLHandler() (URLHandlerResult, error) {
	exe, err := os.Executable()
	if err != nil {
		return URLHandlerResult{}, err
	}
	return registerURL(userURLKeys, exe, false)
}

// RegisterURLHandler is `clack -register-url-handler`, an explicit
// request: Clack's keys, and the legacy key whoever had it. An app the
// user picked with Always in Windows' box still wins; Owner then names it.
func RegisterURLHandler() (URLHandlerResult, error) {
	exe, err := os.Executable()
	if err != nil {
		return URLHandlerResult{}, err
	}
	return registerURL(userURLKeys, exe, true)
}

// UnregisterURLHandler removes Clack's registration: its ProgID,
// Capabilities, RegisteredApplications value and OpenWithProgids entry,
// and the legacy key's handler if a Clack owns it. Another app's legacy
// key is left alone (Owner names it).
func UnregisterURLHandler() (URLHandlerResult, error) {
	return unregisterURL(userURLKeys)
}

func registerURL(r urlKeys, exe string, takeDefault bool) (URLHandlerResult, error) {
	urlMu.Lock()
	defer urlMu.Unlock()
	cmd := `"` + exe + `" "%1"`
	icon := `"` + exe + `",0`
	var res URLHandlerResult
	if !takeDefault {
		res.Owner = r.owner() // before Clack's own keys are written
	}
	var w regWriter
	w.str(r.progID(), "", "URL:slack Protocol")
	w.str(r.progID(), "URL Protocol", "")
	w.str(r.progID()+`\DefaultIcon`, "", icon)
	w.str(r.progID()+`\shell\open\command`, "", cmd)
	w.str(r.caps(), "ApplicationName", urlAppName)
	w.str(r.caps(), "ApplicationDescription", urlAppDesc)
	w.str(r.caps()+`\URLAssociations`, "slack", urlProgID)
	w.str(r.apps(), urlAppName, r.caps())
	if res.Owner == "" {
		w.str(r.legacy(), "", "URL:slack Protocol")
		w.str(r.legacy(), "URL Protocol", "")
		w.str(r.legacy()+`\DefaultIcon`, "", icon)
		w.str(r.legacy()+`\shell\open\command`, "", cmd)
	}
	// Slick also lists itself under the slack key's OpenWithProgids.
	// Clack does too, but never creates a slack key for it: one without a
	// handler could stand in front of the machine's or a Store app's.
	if hasValue(r.legacy(), "URL Protocol") {
		w.none(r.legacy()+`\OpenWithProgids`, urlProgID)
	}
	if w.changed {
		r.notify()
	}
	if takeDefault && w.err == nil {
		res.Owner = r.owner() // what the user chose in Windows' box still wins
	}
	res.Changed, res.Default = w.changed, res.Owner == ""
	return res, w.err
}

func unregisterURL(r urlKeys) (URLHandlerResult, error) {
	urlMu.Lock()
	defer urlMu.Unlock()
	var res URLHandlerResult
	var errs []error
	do := func(done bool, err error) {
		res.Changed = res.Changed || done
		if err != nil {
			errs = append(errs, err)
		}
	}
	do(deleteTree(r.progID()))
	do(deleteTree(r.caps()))
	do(deleteIfEmpty(r.software + `\Clack`))
	do(deleteValue(r.apps(), urlAppName))
	do(deleteValue(r.legacy()+`\OpenWithProgids`, urlProgID))
	do(deleteIfEmpty(r.legacy() + `\OpenWithProgids`))
	if cmd := readString(registry.CURRENT_USER, r.legacy()+`\shell\open\command`, ""); cmd != "" {
		if exe := commandExe(cmd); isClackExe(exe) {
			do(deleteTree(r.legacy() + `\shell`))
			do(deleteTree(r.legacy() + `\DefaultIcon`))
			do(deleteValue(r.legacy(), ""))
			do(deleteValue(r.legacy(), "URL Protocol"))
			do(deleteIfEmpty(r.legacy()))
		} else {
			res.Owner = exe
			// Slack desktop rewrites only the command: an icon Clack left
			// when the key was its own goes
			if isClackExe(commandExe(readString(registry.CURRENT_USER, r.legacy()+`\DefaultIcon`, ""))) {
				do(deleteTree(r.legacy() + `\DefaultIcon`))
			}
		}
	}
	if res.Changed {
		r.notify()
	}
	return res, errors.Join(errs...)
}

// isClackExe reports whether a handler's program is a Clack: Clack.exe
// (installer, portable zip) or a CI build's clack-windows-amd64.exe,
// wherever it is. Slack desktop's slack.exe and Slick.exe aren't.
func isClackExe(p string) bool {
	b := strings.ToLower(filepath.Base(p))
	return strings.HasPrefix(b, "clack") && strings.HasSuffix(b, ".exe")
}

// commandExe is the program a shell\open\command runs: the quoted first
// token, or up to ".exe" or the first space.
func commandExe(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if i := strings.IndexByte(cmd[1:], '"'); i >= 0 {
			return cmd[1 : 1+i]
		}
		return cmd[1:]
	}
	if i := strings.Index(strings.ToLower(cmd), ".exe"); i >= 0 {
		return cmd[:i+4]
	}
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

// The keys Windows keeps the app picked with Always in (read only: they
// are hash-protected, and newer Windows 11 builds add UserChoiceLatest).
var userChoiceKeys = []string{
	`Software\Microsoft\Windows\Shell\Associations\UrlAssociations\slack\UserChoiceLatest`,
	`Software\Microsoft\Windows\Shell\Associations\UrlAssociations\slack\UserChoice`,
}

// shellOwner is another app that opens slack:// for this user, or "": the
// one picked with Always in Windows' box, the legacy key's command as HKCR
// shows it (the user's registration over the machine's), or the program
// the shell itself resolves (AssocQueryString, which also answers for
// Store apps). A Clack, wherever it is, isn't another app.
func shellOwner() string {
	for _, k := range userChoiceKeys {
		id := readString(registry.CURRENT_USER, k, "ProgId")
		if id != "" && !strings.EqualFold(id, urlProgID) && !strings.EqualFold(id, "slack") {
			return id
		}
	}
	if cmd := readString(registry.CLASSES_ROOT, `slack\shell\open\command`, ""); cmd != "" {
		if p := commandExe(cmd); !isClackExe(p) {
			return p
		}
	}
	if p := assocExecutable("slack"); p != "" && !isClackExe(p) && !strings.EqualFold(filepath.Base(p), "OpenWith.exe") {
		return p
	}
	return ""
}

var (
	procAssocQueryStringW = windows.NewLazySystemDLL("shlwapi.dll").NewProc("AssocQueryStringW")
	procSHChangeNotify    = windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")
	procRegSetValueExW    = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegSetValueExW")
)

// assocExecutable is the program the shell starts for scheme's open verb
// (AssocQueryStringW, ASSOCF_IS_PROTOCOL, ASSOCSTR_EXECUTABLE), or "".
func assocExecutable(scheme string) string {
	const (
		assocfIsProtocol   = 0x00001000
		assocstrExecutable = 2
	)
	if procAssocQueryStringW.Find() != nil {
		return ""
	}
	s, err := windows.UTF16PtrFromString(scheme)
	if err != nil {
		return ""
	}
	verb, _ := windows.UTF16PtrFromString("open")
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	hr, _, _ := procAssocQueryStringW.Call(assocfIsProtocol, assocstrExecutable,
		uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if uint32(hr) != 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// assocChanged tells the shell that handlers changed. Default Programs:
// SHChangeNotify(SHCNE_ASSOCCHANGED, SHCNF_DWORD | SHCNF_FLUSH) "is
// required to ensure the proper functioning of system defaults". The
// installer does the same (clack.iss, ChangesAssociations).
func assocChanged() {
	const (
		shcneAssocChanged = 0x08000000
		shcnfDword        = 0x0003
		shcnfFlush        = 0x1000
	)
	if procSHChangeNotify.Find() == nil {
		_, _, _ = procSHChangeNotify.Call(shcneAssocChanged, shcnfDword|shcnfFlush, 0, 0)
	}
}

// regWriter sets HKCU values that differ from what is there, and notes
// whether it changed anything and the first error.
type regWriter struct {
	changed bool
	err     error
}

func (w *regWriter) key(path string) (registry.Key, bool) {
	if w.err != nil {
		return 0, false
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		w.err = fmt.Errorf(`HKCU\%s: %w`, path, err)
		return 0, false
	}
	return k, true
}

func (w *regWriter) str(path, name, val string) {
	k, ok := w.key(path)
	if !ok {
		return
	}
	defer k.Close()
	if cur, typ, err := k.GetStringValue(name); err == nil && typ == registry.SZ && cur == val {
		return
	}
	if err := k.SetStringValue(name, val); err != nil {
		w.err = fmt.Errorf(`HKCU\%s: %w`, path, err)
		return
	}
	w.changed = true
}

// none sets an empty REG_NONE value (OpenWithProgids' kind).
func (w *regWriter) none(path, name string) {
	k, ok := w.key(path)
	if !ok {
		return
	}
	defer k.Close()
	if _, typ, err := k.GetValue(name, nil); err == nil && typ == registry.NONE {
		return
	}
	p, err := windows.UTF16PtrFromString(name)
	if err == nil && procRegSetValueExW.Find() == nil {
		if r, _, _ := procRegSetValueExW.Call(uintptr(k), uintptr(unsafe.Pointer(p)), 0, registry.NONE, 0, 0); r != 0 {
			err = syscall.Errno(r)
		}
	}
	if err != nil {
		w.err = fmt.Errorf(`HKCU\%s: %w`, path, err)
		return
	}
	w.changed = true
}

func readString(root registry.Key, path, name string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	s, _, _ := k.GetStringValue(name)
	return s
}

func hasValue(path, name string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetValue(name, nil)
	return err == nil
}

// deleteTree deletes HKCU\path and everything under it (false: it
// wasn't there).
func deleteTree(path string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf(`HKCU\%s: %w`, path, err)
	}
	subs, err := k.ReadSubKeyNames(-1)
	k.Close()
	if err != nil {
		return false, fmt.Errorf(`HKCU\%s: %w`, path, err)
	}
	for _, s := range subs {
		if _, err := deleteTree(path + `\` + s); err != nil {
			return false, err
		}
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, fmt.Errorf(`HKCU\%s: %w`, path, err)
	}
	return true, nil
}

// deleteValue deletes one value (false: it wasn't there).
func deleteValue(path, name string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf(`HKCU\%s: %w`, path, err)
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf(`HKCU\%s: %w`, path, err)
	}
	return true, nil
}

// deleteIfEmpty deletes HKCU\path if it has no subkeys and no values.
func deleteIfEmpty(path string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf(`HKCU\%s: %w`, path, err)
	}
	st, err := k.Stat()
	k.Close()
	if err != nil {
		return false, fmt.Errorf(`HKCU\%s: %w`, path, err)
	}
	if st.SubKeyCount > 0 || st.ValueCount > 0 {
		return false, nil
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, fmt.Errorf(`HKCU\%s: %w`, path, err)
	}
	return true, nil
}
