package desktop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The asset names of a real release (v0.1.0-alpha.8, release.yml).
var alpha8 = []string{
	"Clack-0.1.0-alpha.8-aarch64.AppImage", "Clack-0.1.0-alpha.8-macos-universal.dmg",
	"Clack-0.1.0-alpha.8-macos-universal.zip", "Clack-0.1.0-alpha.8-windows-amd64-setup.exe",
	"Clack-0.1.0-alpha.8-windows-amd64.zip", "Clack-0.1.0-alpha.8-windows-arm64.zip",
	"Clack-0.1.0-alpha.8-x86_64.AppImage", "clack-0.1.0-alpha.8.aarch64.rpm",
	"clack-0.1.0-alpha.8.x86_64.rpm", "clack_0.1.0-alpha.8_amd64.deb", "clack_0.1.0-alpha.8_arm64.deb",
	"SHA256SUMS",
}

func release(tag string, names ...string) Release {
	r := Release{Tag: tag}
	for _, n := range names {
		r.Assets = append(r.Assets, Asset{Name: n, URL: "https://github.com/some-du6e/yoloslackgoclient/releases/download/" + tag + "/" + n, Size: 10})
	}
	return r
}

func TestDetectInstall(t *testing.T) {
	files := map[string]bool{"/home/k/Apps/Clack.AppImage": true}
	globs := map[string][]string{`C:\Users\k\AppData\Local\Programs\Clack\unins*.exe`: {`C:\Users\k\AppData\Local\Programs\Clack\unins000.exe`}}
	owners := map[string]InstallKind{"/usr/bin/clack": KindDeb, "/usr/local/bin/clack": KindUnknown, "/opt/clack/clack": KindRPM}
	for _, c := range []struct {
		goos, exe, appimage string
		kind                InstallKind
		target              string
	}{
		{"darwin", "/Applications/Clack.app/Contents/MacOS/clack", "", KindMacApp, "/Applications/Clack.app"},
		{"darwin", "/Users/k/Downloads/Clack 2.app/Contents/MacOS/clack", "", KindMacApp, "/Users/k/Downloads/Clack 2.app"},
		{"darwin", "/Users/k/go/bin/clack", "", KindUnknown, ""},
		{"windows", `C:\Users\k\AppData\Local\Programs\Clack\Clack.exe`, "", KindWinInstaller, `C:\Users\k\AppData\Local\Programs\Clack\Clack.exe`},
		{"windows", `D:\Tools\Clack\Clack.exe`, "", KindWinPortable, `D:\Tools\Clack\Clack.exe`},
		{"linux", "/tmp/.mount_ClackXYZ/usr/bin/clack", "/home/k/Apps/Clack.AppImage", KindAppImage, "/home/k/Apps/Clack.AppImage"},
		{"linux", "/usr/bin/clack", "/home/k/gone.AppImage", KindDeb, ""}, // a stale $APPIMAGE
		{"linux", "/usr/bin/clack", "", KindDeb, ""},
		{"linux", "/opt/clack/clack", "", KindRPM, ""},
		{"linux", "/usr/local/bin/clack", "", KindLinuxBinary, ""},
		{"freebsd", "/usr/local/bin/clack", "", KindUnknown, ""},
	} {
		if c.goos == "windows" && runtime.GOOS != "windows" || c.goos != "windows" && runtime.GOOS == "windows" {
			continue // filepath separators
		}
		in := detectInstall(probe{
			goos: c.goos, goarch: "amd64", exe: c.exe,
			getenv: func(k string) string {
				if k == "APPIMAGE" {
					return c.appimage
				}
				return ""
			},
			glob:   func(p string) ([]string, error) { return globs[p], nil },
			isFile: func(p string) bool { return files[p] },
			owner:  func(p string) InstallKind { return owners[p] },
		})
		if in.Kind != c.kind || in.Target != c.target {
			t.Errorf("%s %s: %q %q, want %q %q", c.goos, c.exe, in.Kind, in.Target, c.kind, c.target)
		}
	}
}

func TestPickAsset(t *testing.T) {
	r := release("v0.1.0-alpha.8", alpha8...)
	for _, c := range []struct {
		kind   InstallKind
		goarch string
		want   string
	}{
		{KindMacApp, "arm64", "Clack-0.1.0-alpha.8-macos-universal.dmg"},
		{KindMacApp, "amd64", "Clack-0.1.0-alpha.8-macos-universal.dmg"},
		{KindWinInstaller, "amd64", "Clack-0.1.0-alpha.8-windows-amd64-setup.exe"},
		{KindWinPortable, "amd64", "Clack-0.1.0-alpha.8-windows-amd64.zip"},
		{KindWinPortable, "arm64", "Clack-0.1.0-alpha.8-windows-arm64.zip"},
		{KindAppImage, "amd64", "Clack-0.1.0-alpha.8-x86_64.AppImage"},
		{KindAppImage, "arm64", "Clack-0.1.0-alpha.8-aarch64.AppImage"},
		{KindLinuxBinary, "amd64", "Clack-0.1.0-alpha.8-x86_64.AppImage"},
		{KindDeb, "amd64", "clack_0.1.0-alpha.8_amd64.deb"},
		{KindDeb, "arm64", "clack_0.1.0-alpha.8_arm64.deb"},
		{KindRPM, "amd64", "clack-0.1.0-alpha.8.x86_64.rpm"},
		{KindRPM, "arm64", "clack-0.1.0-alpha.8.aarch64.rpm"},
		{KindUnknown, "amd64", ""},
		{KindAppImage, "riscv64", ""},
	} {
		a, err := Install{Kind: c.kind, GOARCH: c.goarch}.PickAsset(r)
		if a.Name != c.want || (c.want == "") != errors.Is(err, ErrNoAsset) {
			t.Errorf("%s/%s: %q %v, want %q", c.kind, c.goarch, a.Name, err, c.want)
		}
	}
	// a release without the dmg falls back to the zip
	noDMG := release("v0.2.0", "Clack-0.2.0-macos-universal.zip", "SHA256SUMS")
	if a, _ := (Install{Kind: KindMacApp}).PickAsset(noDMG); a.Name != "Clack-0.2.0-macos-universal.zip" {
		t.Errorf("mac without dmg: %q", a.Name)
	}
	// a name that only looks right isn't taken
	odd := release("v0.2.0", "Clack-0.2.0-macos-universal.dmg.sig", "SHA256SUMS")
	if _, err := (Install{Kind: KindMacApp}).PickAsset(odd); !errors.Is(err, ErrNoAsset) {
		t.Errorf("picked a lookalike: %v", err)
	}
}

func TestParseReleases(t *testing.T) {
	list := `[{"tag_name":"v0.1.0-alpha.7","prerelease":true},{"tag_name":"v0.2.0","draft":true},
		{"tag_name":"v0.1.0-alpha.10","prerelease":true},{"tag_name":"nightly"},{"tag_name":"v0.1.0-alpha.9","prerelease":true}]`
	r, err := parseReleases([]byte(list))
	if err != nil || r.Tag != "v0.1.0-alpha.10" {
		t.Errorf("list: %q %v", r.Tag, err)
	}
	r, err = parseReleases([]byte(`{"tag_name":"v0.3.0","assets":[{"name":"SHA256SUMS","browser_download_url":"https://x/y","size":5}]}`))
	if err != nil || r.Tag != "v0.3.0" || len(r.Assets) != 1 || r.Assets[0].Size != 5 {
		t.Errorf("object: %+v %v", r, err)
	}
	if _, err := parseReleases([]byte(`[{"tag_name":"v1.0.0","draft":true}]`)); err == nil {
		t.Error("only drafts: no error")
	}
	// pre-release builds follow the pre-release channel
	if u := releasesURL("v0.1.0-alpha.8"); u != ReleaseListURL {
		t.Errorf("pre-release build checks %s", u)
	}
	if u := releasesURL("v0.1.0"); u != ReleasesURL {
		t.Errorf("release build checks %s", u)
	}
}

// fakeReleases is a local GitHub: the releases API, release downloads (with
// Range support) and SHA256SUMS.
type fakeReleases struct {
	srv   *httptest.Server
	mu    sync.Mutex
	files map[string][]byte // served bytes
	sums  map[string][]byte // what SHA256SUMS lists (default: files)
	// cut: serve only this many bytes of the next n requests for a file,
	// then drop the connection.
	cut      map[string]int
	cutTimes map[string]int
	ranges   []string // Range headers seen
	extra    map[string][]byte
	gets     map[string]int
}

func newFakeReleases(t *testing.T) *fakeReleases {
	f := &fakeReleases{files: map[string][]byte{}, sums: map[string][]byte{}, cut: map[string]int{}, cutTimes: map[string]int{}, extra: map[string][]byte{}, gets: map[string]int{}}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeReleases) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := strings.TrimPrefix(r.URL.Path, "/download/")
	f.gets[name]++
	if strings.HasPrefix(r.URL.Path, "/redirect/") {
		http.Redirect(w, r, r.URL.Query().Get("to"), http.StatusFound)
		return
	}
	if name == "SHA256SUMS" {
		var b bytes.Buffer
		for n, body := range f.files {
			if s, ok := f.sums[n]; ok {
				if s != nil {
					fmt.Fprintf(&b, "%x  %s\n", sha256.Sum256(s), n)
				}
				continue
			}
			fmt.Fprintf(&b, "%x  %s\n", sha256.Sum256(body), n)
		}
		w.Write(b.Bytes())
		return
	}
	body, ok := f.files[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	body = append(body, f.extra[name]...)
	f.ranges = append(f.ranges, r.Header.Get("Range"))
	if n := f.cut[name]; n > 0 && f.cutTimes[name] > 0 {
		f.cutTimes[name]--
		start := 0
		if rg := r.Header.Get("Range"); rg != "" {
			fmt.Sscanf(rg, "bytes=%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
			w.Header().Set("Content-Length", fmt.Sprint(len(body)-start))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		}
		w.Write(body[start:min(len(body), start+n)])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // drop the connection mid-body
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
}

func (f *fakeReleases) url(name string) string { return f.srv.URL + "/download/" + name }

// release lists the files with their real sizes (or size, if given).
func (f *fakeReleases) release(tag string, names ...string) Release {
	r := Release{Tag: tag}
	for _, n := range names {
		r.Assets = append(r.Assets, Asset{Name: n, URL: f.url(n), Size: int64(len(f.files[n]))})
	}
	r.Assets = append(r.Assets, Asset{Name: "SHA256SUMS", URL: f.url("SHA256SUMS")})
	return r
}

func (f *fakeReleases) updater(t *testing.T) *Updater {
	return &Updater{Dir: t.TempDir(), UserAgent: "clack/v0.1.0", Transport: f.srv.Client().Transport,
		Hosts: []string{"127.0.0.1"}, Backoff: time.Millisecond}
}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

// noInstall fails the test if anything runs a command (an installer, a
// mount, a relaunch) or starts the helper.
func noInstall(t *testing.T) {
	t.Helper()
	oldRun, oldStart := runCmd, startCmd
	runCmd = func(ctx context.Context, name string, args ...string) error {
		t.Errorf("ran %s %v without the click", name, args)
		return nil
	}
	startCmd = func(name string, args ...string) error {
		t.Errorf("started %s %v without the click", name, args)
		return nil
	}
	t.Cleanup(func() { runCmd, startCmd = oldRun, oldStart })
}

func TestDownloadVerifies(t *testing.T) {
	noInstall(t)
	f := newFakeReleases(t)
	const name = "Clack-0.2.0-macos-universal.dmg"
	f.files[name] = payload(100_000)
	u := f.updater(t)
	r := f.release("v0.2.0", name)
	pkg, err := u.Download(context.Background(), r, r.Assets[0])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(pkg.Path)
	if !bytes.Equal(b, f.files[name]) || pkg.Path != u.Path("v0.2.0", name) || pkg.SHA256 != fmt.Sprintf("%x", sha256.Sum256(b)) {
		t.Fatalf("package %+v", pkg)
	}
	// already there: not fetched again
	gets := f.gets[name]
	if _, err := u.Download(context.Background(), r, r.Assets[0]); err != nil || f.gets[name] != gets {
		t.Errorf("second download: %v, %d fetches", err, f.gets[name]-gets)
	}
	// a newer release's download replaces the older one's directory
	f.files["Clack-0.3.0-macos-universal.dmg"] = payload(10)
	r3 := f.release("v0.3.0", "Clack-0.3.0-macos-universal.dmg")
	if _, err := u.Download(context.Background(), r3, r3.Assets[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(u.Dir, "v0.2.0")); !os.IsNotExist(err) {
		t.Errorf("old release's download kept: %v", err)
	}
}

func TestDownloadTampered(t *testing.T) {
	noInstall(t)
	f := newFakeReleases(t)
	const name = "clack_0.2.0_amd64.deb"
	f.files[name] = payload(5000)
	f.sums[name] = append(payload(4999), 1) // SHA256SUMS lists other bytes
	u := f.updater(t)
	r := f.release("v0.2.0", name)
	_, err := u.Download(context.Background(), r, r.Assets[0])
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("tampered: %v", err)
	}
	for _, p := range []string{u.Path("v0.2.0", name), u.Path("v0.2.0", name) + ".part"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s kept after a mismatch", filepath.Base(p))
		}
	}
	// the bytes change after a good download: Prepare re-checks before installing
	f.sums = map[string][]byte{}
	pkg, err := u.Download(context.Background(), r, r.Assets[0])
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(pkg.Path, []byte("evil"), 0o600)
	if _, err := Prepare(context.Background(), Install{Kind: KindDeb}, pkg); !errors.Is(err, ErrChecksum) {
		t.Errorf("Prepare of a changed package: %v", err)
	}
	// no entry in SHA256SUMS, or no SHA256SUMS at all: never offered
	f.sums[name] = nil
	os.Remove(pkg.Path)
	if _, err := u.Download(context.Background(), r, r.Assets[0]); !errors.Is(err, ErrNoChecksum) {
		t.Errorf("missing entry: %v", err)
	}
	noSums := Release{Tag: "v0.2.0", Assets: r.Assets[:1]}
	if _, err := u.Download(context.Background(), noSums, r.Assets[0]); !errors.Is(err, ErrNoChecksum) {
		t.Errorf("no SHA256SUMS: %v", err)
	}
	if _, err := os.Stat(u.Path("v0.2.0", name)); !os.IsNotExist(err) {
		t.Error("an unverified package is on disk")
	}
}

func TestParseSums(t *testing.T) {
	h := strings.Repeat("ab", 32)
	sums := []byte("# comment\n" + h + "  a.dmg\n" + strings.Repeat("cd", 32) + " *b.zip\nzz  c.deb\n" + h + "  a.dmg.sig\n")
	if s, ok := ParseSums(sums, "a.dmg"); !ok || s != h {
		t.Errorf("a.dmg: %q %v", s, ok)
	}
	if s, ok := ParseSums(sums, "b.zip"); !ok || s != strings.Repeat("cd", 32) {
		t.Errorf("b.zip (binary mode): %q %v", s, ok)
	}
	for _, n := range []string{"c.deb", "missing", "a"} {
		if _, ok := ParseSums(sums, n); ok {
			t.Errorf("%s: found", n)
		}
	}
}

// A dropped connection resumes with a Range request where it stopped.
func TestDownloadResumes(t *testing.T) {
	noInstall(t)
	f := newFakeReleases(t)
	const name = "Clack-0.2.0-x86_64.AppImage"
	f.files[name] = payload(300_000)
	f.cut[name], f.cutTimes[name] = 70_000, 2 // two drops
	u := f.updater(t)
	r := f.release("v0.2.0", name)
	pkg, err := u.Download(context.Background(), r, r.Assets[0])
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(pkg.Path); !bytes.Equal(b, f.files[name]) {
		t.Fatal("resumed download differs")
	}
	if len(f.ranges) != 3 || f.ranges[0] != "" || !strings.HasPrefix(f.ranges[1], "bytes=") || f.ranges[1] == f.ranges[2] {
		t.Errorf("Range headers %q", f.ranges)
	}
	// out of attempts: the error, and the partial file kept for next time
	f.files["b.AppImage"] = payload(300_000)
	f.cut["b.AppImage"], f.cutTimes["b.AppImage"] = 1000, 10
	u.Attempts = 2
	rb := f.release("v0.2.0", "b.AppImage")
	if _, err := u.Download(context.Background(), rb, rb.Assets[0]); err == nil {
		t.Fatal("no error after the attempts ran out")
	}
	if fi, err := os.Stat(u.Path("v0.2.0", "b.AppImage") + ".part"); err != nil || fi.Size() != 2000 {
		t.Errorf("partial download: %v %v", fi, err)
	}
	f.cutTimes["b.AppImage"] = 0
	if _, err := u.Download(context.Background(), rb, rb.Assets[0]); err != nil {
		t.Errorf("finishing the partial download: %v", err)
	}
	if last := f.ranges[len(f.ranges)-1]; last != "bytes=2000-" {
		t.Errorf("didn't resume from 2000: %q", last)
	}
}

func TestDownloadCap(t *testing.T) {
	noInstall(t)
	f := newFakeReleases(t)
	f.files["big.dmg"] = payload(50_000)
	u := f.updater(t)
	u.MaxSize = 10_000
	r := f.release("v0.2.0", "big.dmg")
	// the release declares more than the cap: not even fetched
	if _, err := u.Download(context.Background(), r, r.Assets[0]); !errors.Is(err, ErrTooLarge) || f.gets["big.dmg"] != 0 {
		t.Errorf("declared too large: %v, %d fetches", err, f.gets["big.dmg"])
	}
	// the server sends more than the release declared: cut off, not kept
	f.files["small.dmg"] = payload(5_000)
	f.extra["small.dmg"] = payload(20_000)
	r = f.release("v0.2.0", "small.dmg")
	if _, err := u.Download(context.Background(), r, r.Assets[0]); !errors.Is(err, ErrTooLarge) {
		t.Errorf("body past the declared size: %v", err)
	}
	if _, err := os.Stat(u.Path("v0.2.0", "small.dmg") + ".part"); !os.IsNotExist(err) {
		t.Error("oversized partial kept")
	}
	// no declared size: the cap applies
	delete(f.extra, "small.dmg")
	f.files["nosize.dmg"] = payload(20_000)
	r = f.release("v0.2.0", "nosize.dmg")
	r.Assets[0].Size = 0
	if _, err := u.Download(context.Background(), r, r.Assets[0]); !errors.Is(err, ErrTooLarge) {
		t.Errorf("undeclared size over the cap: %v", err)
	}
}

func TestDownloadHosts(t *testing.T) {
	noInstall(t)
	f := newFakeReleases(t)
	f.files["a.dmg"] = payload(100)
	r := f.release("v0.2.0", "a.dmg")
	// only the allowed hosts, https only
	u := f.updater(t)
	u.Hosts = nil // GitHub's only
	if _, err := u.Download(context.Background(), r, r.Assets[0]); !errors.Is(err, ErrHost) {
		t.Errorf("127.0.0.1 with GitHub's hosts: %v", err)
	}
	u = f.updater(t)
	plain := r
	plain.Assets = []Asset{{Name: "a.dmg", URL: strings.Replace(f.url("a.dmg"), "https:", "http:", 1), Size: 100}, r.Assets[1]}
	if _, err := u.Download(context.Background(), plain, plain.Assets[0]); !errors.Is(err, ErrHost) {
		t.Errorf("plain http: %v", err)
	}
	// a redirect to another host is refused
	redir := r
	redir.Assets = []Asset{{Name: "a.dmg", URL: f.srv.URL + "/redirect/x?to=https://evil.example/a.dmg", Size: 100}, r.Assets[1]}
	if _, err := u.Download(context.Background(), redir, redir.Assets[0]); !errors.Is(err, ErrHost) {
		t.Errorf("redirect off GitHub: %v", err)
	}
	for _, c := range []struct {
		url string
		ok  bool
	}{
		{"https://github.com/some-du6e/yoloslackgoclient/releases/download/v1/x", true},
		{"https://objects.githubusercontent.com/x", true},
		{"https://release-assets.githubusercontent.com/x", true},
		{"http://github.com/x", false},
		{"https://github.com.evil.example/x", false},
		{"https://api.github.com/x", false},
		{"https://slack.com/x", false},
	} {
		p, _ := url.Parse(c.url)
		if err := (&Updater{}).checkURL(p); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.url, err)
		}
	}
	// odd tag or file names never become paths
	bad := Release{Tag: "../x", Assets: r.Assets}
	if _, err := u.Download(context.Background(), bad, r.Assets[0]); err == nil {
		t.Error("tag ../x accepted")
	}
}

func TestLatestReleaseFromFake(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		json.NewEncoder(w).Encode([]Release{{Tag: "v0.1.0-alpha.9", Prerelease: true, Assets: []Asset{{Name: "SHA256SUMS", URL: "https://github.com/x", Size: 1}}}, {Tag: "v0.1.0-alpha.8"}})
	}))
	defer srv.Close()
	t.Setenv("CLACK_UPDATE_URL", srv.URL)
	r, err := LatestRelease(context.Background(), "v0.1.0-alpha.8")
	if err != nil || r.Tag != "v0.1.0-alpha.9" || len(r.Assets) != 1 || !strings.Contains(r.URL, "/releases/tag/v0.1.0-alpha.9") {
		t.Fatalf("%+v %v", r, err)
	}
	if ua != "clack/v0.1.0-alpha.8" {
		t.Errorf("User-Agent %q", ua)
	}
}

// recordCmds replaces the command runners, recording what an install runs.
type recordCmds struct {
	mu      sync.Mutex
	ran     [][]string
	started [][]string
	fail    map[string]error // by command base name
}

func record(t *testing.T) *recordCmds {
	rc := &recordCmds{fail: map[string]error{}}
	oldRun, oldStart, oldLook := runCmd, startCmd, lookPath
	runCmd = func(ctx context.Context, name string, args ...string) error {
		rc.mu.Lock()
		defer rc.mu.Unlock()
		rc.ran = append(rc.ran, append([]string{name}, args...))
		return rc.fail[filepath.Base(name)]
	}
	startCmd = func(name string, args ...string) error {
		rc.mu.Lock()
		defer rc.mu.Unlock()
		rc.started = append(rc.started, append([]string{name}, args...))
		return nil
	}
	lookPath = func(name string) (string, error) {
		switch name {
		case "pkexec", "apt-get", "dnf", "xdg-open":
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { runCmd, startCmd, lookPath = oldRun, oldStart, oldLook })
	return rc
}

func pkgFor(t *testing.T, name string, body []byte) Package {
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	os.WriteFile(p, body, 0o600)
	h := sha256.Sum256(body)
	return Package{Tag: "v0.2.0", Name: name, Path: p, SHA256: hex.EncodeToString(h[:])}
}

func TestPrepareWindowsInstaller(t *testing.T) {
	rc := record(t)
	pkg := pkgFor(t, "Clack-0.2.0-windows-amd64-setup.exe", []byte("MZsetup"))
	in := Install{Kind: KindWinInstaller, Exe: filepath.Join(t.TempDir(), "Clack.exe")}
	p, err := Prepare(context.Background(), in, pkg)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{pkg.Path}, "/VERYSILENT", "/CURRENTUSER", "/SUPPRESSMSGBOXES", "/NORESTART", "/LOG="+pkg.Path+".log")
	if fmt.Sprint(p.Run) != fmt.Sprint(want) || fmt.Sprint(p.Relaunch) != fmt.Sprint([]string{in.Exe}) || p.PID != os.Getpid() {
		t.Errorf("plan %+v", p)
	}
	if len(rc.ran)+len(rc.started) != 0 {
		t.Errorf("Prepare ran %v %v; the installer runs after Clack exits", rc.ran, rc.started)
	}
	// the helper runs it, then relaunches
	waitExit = func(int, time.Duration) bool { return true }
	t.Cleanup(func() { waitExit = waitPID })
	if err := applyPlan(p, time.Second); err != nil {
		t.Fatal(err)
	}
	if len(rc.ran) != 1 || rc.ran[0][0] != pkg.Path || len(rc.started) != 1 || rc.started[0][0] != in.Exe {
		t.Errorf("ran %v, started %v", rc.ran, rc.started)
	}
}

func TestPrepareWindowsPortable(t *testing.T) {
	rc := record(t)
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for name, body := range map[string]string{"Clack/Clack.exe": "MZnew", "Clack/README.txt": "readme", "Clack.exe": "MZwrong"} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	pkg := pkgFor(t, "Clack-0.2.0-windows-amd64.zip", zb.Bytes())
	dir := t.TempDir()
	exe := filepath.Join(dir, "Clack.exe")
	os.WriteFile(exe, []byte("MZold"), 0o755)
	in := Install{Kind: KindWinPortable, Exe: exe, Target: exe}
	p, err := Prepare(context.Background(), in, pkg)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "MZold" {
		t.Fatal("Prepare replaced the running exe")
	}
	waitExit = func(int, time.Duration) bool { return true }
	t.Cleanup(func() { waitExit = waitPID })
	if err := applyPlan(p, time.Second); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "MZnew" {
		t.Errorf("after the swap: %q", b)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("left behind in Clack's folder: %v", ents)
	}
	if len(rc.started) != 1 || rc.started[0][0] != exe {
		t.Errorf("relaunch %v", rc.started)
	}
}

func TestPrepareAppImage(t *testing.T) {
	rc := record(t)
	pkg := pkgFor(t, "Clack-0.2.0-x86_64.AppImage", []byte("\x7fELFnew"))
	dir := t.TempDir()
	ai := filepath.Join(dir, "Clack-0.1.0-x86_64.AppImage") // the user's file keeps its name
	os.WriteFile(ai, []byte("\x7fELFold"), 0o755)
	p, err := Prepare(context.Background(), Install{Kind: KindAppImage, Target: ai}, pkg)
	if err != nil {
		t.Fatal(err)
	}
	waitExit = func(int, time.Duration) bool { return true }
	t.Cleanup(func() { waitExit = waitPID })
	if err := applyPlan(p, time.Second); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(ai)
	if b, _ := os.ReadFile(ai); string(b) != "\x7fELFnew" || (runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0) {
		t.Errorf("AppImage after the swap: %q %v", b, fi.Mode())
	}
	if len(rc.started) != 1 || rc.started[0][0] != ai {
		t.Errorf("relaunch %v", rc.started)
	}
	// a failed swap puts the old copy back
	os.WriteFile(ai, []byte("\x7fELFold"), 0o755)
	if err := installPlan(Plan{Swap: &Swap{Target: ai, New: filepath.Join(dir, "missing")}}); err == nil {
		t.Error("swap from a missing file succeeded")
	}
	if b, _ := os.ReadFile(ai); string(b) != "\x7fELFold" {
		t.Errorf("not rolled back: %q", b)
	}
}

type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitCode) ExitCode() int { return int(e) }

func TestPrepareLinuxPackages(t *testing.T) {
	rc := record(t)
	deb := pkgFor(t, "clack_0.2.0_amd64.deb", []byte("!<arch>"))
	in := Install{Kind: KindDeb, Exe: "/usr/bin/clack"}
	p, err := Prepare(context.Background(), in, deb)
	if err != nil {
		t.Fatal(err)
	}
	// pkexec asks for the password; nothing installs as root silently
	if want := []string{"/usr/bin/pkexec", "/usr/bin/apt-get", "install", "-y", deb.Path}; fmt.Sprint(rc.ran) != fmt.Sprint([][]string{want}) {
		t.Errorf("ran %v", rc.ran)
	}
	if fmt.Sprint(p.Relaunch) != "[/usr/bin/clack]" || p.Swap != nil {
		t.Errorf("plan %+v", p)
	}
	rpm := pkgFor(t, "clack-0.2.0.x86_64.rpm", []byte("rpm"))
	rc.ran = nil
	if _, err := Prepare(context.Background(), Install{Kind: KindRPM, Exe: "/usr/bin/clack"}, rpm); err != nil || rc.ran[0][1] != "/usr/bin/dnf" {
		t.Errorf("rpm: %v %v", err, rc.ran)
	}
	// the password dialog dismissed: cancelled, nothing else opens
	rc.ran, rc.started = nil, nil
	rc.fail["pkexec"] = exitCode(126)
	if _, err := Prepare(context.Background(), in, deb); !errors.Is(err, ErrCancelled) || len(rc.started) != 0 {
		t.Errorf("dismissed: %v %v", err, rc.started)
	}
	// no polkit agent: the software installer opens instead
	if runtime.GOOS != "linux" {
		return
	}
	rc.fail["pkexec"] = exitCode(127)
	if _, err := Prepare(context.Background(), in, deb); !errors.Is(err, ErrManual) || fmt.Sprint(rc.started) != fmt.Sprint([][]string{{"xdg-open", deb.Path}}) {
		t.Errorf("no agent: %v %v", err, rc.started)
	}
}

// waitPID sees a real process exit.
func TestWaitPID(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSleep$")
	cmd.Env = append(os.Environ(), "CLACK_TEST_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }() // reap it (Unix)
	if waitPID(cmd.Process.Pid, 200*time.Millisecond) {
		t.Error("waitPID returned while the process runs")
	}
	if !waitPID(cmd.Process.Pid, 20*time.Second) {
		t.Error("waitPID timed out after the process exited")
	}
	<-done
}

func TestHelperSleep(t *testing.T) {
	if os.Getenv("CLACK_TEST_SLEEP") == "" {
		t.Skip("helper process for TestWaitPID")
	}
	time.Sleep(1500 * time.Millisecond)
}

// The helper's whole run through ApplyUpdate: plan file in, swap, log.
func TestApplyUpdateFromPlanFile(t *testing.T) {
	rc := record(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "Clack.exe")
	os.WriteFile(target, []byte("old"), 0o755)
	stage, _ := os.MkdirTemp(dir, stagePrefix)
	os.WriteFile(filepath.Join(stage, "Clack.exe"), []byte("new"), 0o755)
	plan := Plan{Tag: "v0.2.0", Stage: stage, Swap: &Swap{Target: target, New: filepath.Join(stage, "Clack.exe")}, Relaunch: []string{target}}
	b, _ := json.Marshal(plan)
	hd := t.TempDir()
	os.WriteFile(filepath.Join(hd, "plan.json"), b, 0o600)
	if err := ApplyUpdate(filepath.Join(hd, "plan.json")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Errorf("target %q", b)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Error("stage left behind")
	}
	if l, _ := os.ReadFile(filepath.Join(hd, "update.log")); !strings.Contains(string(l), "installed v0.2.0") {
		t.Errorf("log %q", l)
	}
	if len(rc.started) != 1 {
		t.Errorf("relaunch %v", rc.started)
	}
}

// The helper waits for the real process to exit before it swaps the
// executable (on Windows the running exe can't be replaced).
func TestApplyWaitsForExit(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "Clack")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := copyFile(os.Args[0], exe, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestHelperSleep$")
	cmd.Env = append(os.Environ(), "CLACK_TEST_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	stage, _ := os.MkdirTemp(dir, stagePrefix)
	os.WriteFile(filepath.Join(stage, filepath.Base(exe)), []byte("new"), 0o755)
	p := Plan{PID: cmd.Process.Pid, Stage: stage, Swap: &Swap{Target: exe, New: filepath.Join(stage, filepath.Base(exe))}}
	if err := applyPlan(p, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Error("swapped while the process still ran")
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("exe after the swap: %d bytes", len(b))
	}
	if _, err := os.Stat(exe + ".clack-old"); !os.IsNotExist(err) {
		t.Errorf("old copy left: %v", err)
	}
	// a process that doesn't exit: nothing is touched
	cmd = exec.Command(os.Args[0], "-test.run=^TestHelperSleep$")
	cmd.Env = append(os.Environ(), "CLACK_TEST_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	os.WriteFile(filepath.Join(stage+"2"), []byte("newer"), 0o755)
	if err := applyPlan(Plan{PID: cmd.Process.Pid, Swap: &Swap{Target: exe, New: stage + "2"}}, 200*time.Millisecond); err == nil {
		t.Error("no error when Clack didn't exit")
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Error("swapped although Clack didn't exit")
	}
}
