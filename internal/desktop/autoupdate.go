package desktop

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack/tlsfp"
)

// The auto-updater: when the update check finds a newer release, Updater
// downloads the one package that matches how this copy was installed,
// checks it against the release's SHA256SUMS, and keeps it in the cache.
// Nothing is installed until the user clicks "Install and restart"
// (install.go). Releases aren't code-signed yet: the checksum catches a
// corrupt or truncated download, not a compromised repository
// (docs/SECURITY.md D-3).

// InstallKind is how this copy of Clack was installed, which decides the
// package an update downloads and how it is installed.
type InstallKind string

const (
	KindUnknown      InstallKind = ""                  // no automatic update (a bare macOS binary, other OSes)
	KindMacApp       InstallKind = "macos-app"         // Clack.app (from the .dmg or .zip)
	KindWinInstaller InstallKind = "windows-installer" // the Inno Setup per-user install
	KindWinPortable  InstallKind = "windows-portable"  // Clack.exe from the portable .zip
	KindAppImage     InstallKind = "appimage"          // running from an AppImage ($APPIMAGE)
	KindDeb          InstallKind = "deb"               // /usr/bin/clack owned by dpkg
	KindRPM          InstallKind = "rpm"               // /usr/bin/clack owned by rpm
	KindLinuxBinary  InstallKind = "linux-binary"      // anything else on Linux: gets the AppImage
)

// Install describes this copy of Clack.
type Install struct {
	Kind   InstallKind
	GOOS   string
	GOARCH string
	// Exe is the running executable (symlinks resolved).
	Exe string
	// Target is what an update replaces: the .app bundle, Clack.exe, the
	// AppImage file. Empty for packages the system installer handles and
	// for a Linux binary (its AppImage goes to ~/Applications).
	Target string
}

// probe is what DetectInstall looks at, replaceable in tests.
type probe struct {
	goos, goarch, exe string
	getenv            func(string) string
	glob              func(string) ([]string, error)
	isFile            func(string) bool
	owner             func(path string) InstallKind // the package manager that owns path
}

// DetectInstall works out how this copy was installed.
func DetectInstall() Install {
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return detectInstall(probe{
		goos: runtime.GOOS, goarch: runtime.GOARCH, exe: exe,
		getenv: os.Getenv, glob: filepath.Glob,
		isFile: func(p string) bool { fi, err := os.Stat(p); return err == nil && fi.Mode().IsRegular() },
		owner:  packageOwner,
	})
}

func detectInstall(p probe) Install {
	in := Install{GOOS: p.goos, GOARCH: p.goarch, Exe: p.exe}
	switch p.goos {
	case "darwin":
		// …/Clack.app/Contents/MacOS/clack
		macos := filepath.Dir(p.exe)
		contents := filepath.Dir(macos)
		bundle := filepath.Dir(contents)
		if filepath.Base(macos) == "MacOS" && filepath.Base(contents) == "Contents" && strings.HasSuffix(bundle, ".app") {
			in.Kind, in.Target = KindMacApp, bundle
		}
	case "windows":
		// Inno Setup leaves its uninstaller (unins000.exe) next to Clack.exe.
		if m, _ := p.glob(filepath.Join(filepath.Dir(p.exe), "unins*.exe")); len(m) > 0 {
			in.Kind = KindWinInstaller
		} else {
			in.Kind = KindWinPortable
		}
		in.Target = p.exe
	case "linux":
		if ai := p.getenv("APPIMAGE"); ai != "" && filepath.IsAbs(ai) && p.isFile(ai) {
			in.Kind, in.Target = KindAppImage, ai
			break
		}
		if k := p.owner(p.exe); k == KindDeb || k == KindRPM {
			in.Kind = k
			break
		}
		in.Kind = KindLinuxBinary
	}
	return in
}

// packageOwner asks dpkg, then rpm, whether they installed path.
func packageOwner(path string) InstallKind {
	if !strings.HasPrefix(path, "/usr/") && !strings.HasPrefix(path, "/opt/") {
		return KindUnknown
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := exec.LookPath("dpkg-query"); err == nil {
		if exec.CommandContext(ctx, "dpkg-query", "-S", path).Run() == nil {
			return KindDeb
		}
	}
	if _, err := exec.LookPath("rpm"); err == nil {
		if exec.CommandContext(ctx, "rpm", "-qf", path).Run() == nil {
			return KindRPM
		}
	}
	return KindUnknown
}

// ErrNoAsset: the release has no package for this install.
var ErrNoAsset = errors.New("update: no package for this install")

// AssetNames are the release files that update this install, preferred
// first (release.yml's names; <ver> is the tag without the v).
func (in Install) AssetNames(tag string) []string {
	ver := strings.TrimPrefix(tag, "v")
	linuxArch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[in.GOARCH]
	switch in.Kind {
	case KindMacApp:
		return []string{"Clack-" + ver + "-macos-universal.dmg", "Clack-" + ver + "-macos-universal.zip"}
	case KindWinInstaller: // the installer is amd64 only (and runs on arm64 under emulation)
		return []string{"Clack-" + ver + "-windows-amd64-setup.exe"}
	case KindWinPortable:
		return []string{"Clack-" + ver + "-windows-" + in.GOARCH + ".zip"}
	case KindAppImage, KindLinuxBinary:
		if linuxArch != "" {
			return []string{"Clack-" + ver + "-" + linuxArch + ".AppImage"}
		}
	case KindDeb:
		return []string{"clack_" + ver + "_" + in.GOARCH + ".deb"}
	case KindRPM:
		if linuxArch != "" {
			return []string{"clack-" + ver + "." + linuxArch + ".rpm"}
		}
	}
	return nil
}

// PickAsset returns the release's package for this install.
func (in Install) PickAsset(r Release) (Asset, error) {
	for _, n := range in.AssetNames(r.Tag) {
		for _, a := range r.Assets {
			if a.Name == n {
				return a, nil
			}
		}
	}
	return Asset{}, fmt.Errorf("%w (%s, %s/%s, %s)", ErrNoAsset, in.Kind, in.GOOS, in.GOARCH, r.Tag)
}

// Download errors.
var (
	ErrNoChecksum = errors.New("update: the release's SHA256SUMS has no entry for this package")
	ErrChecksum   = errors.New("update: the download doesn't match the release's SHA256SUMS")
	ErrTooLarge   = errors.New("update: the download is larger than allowed")
	ErrHost       = errors.New("update: download host not allowed")
)

// GitHubHosts are the only hosts a release download may come from:
// github.com's release URL redirects to a signed URL on one of the asset
// hosts.
var GitHubHosts = []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}

// Updater downloads and verifies update packages.
type Updater struct {
	// Dir holds the downloads, one directory per release tag.
	Dir string
	// UserAgent is Clack's own ("clack/<version>"), as on the update check.
	UserAgent string
	// Transport defaults to tlsfp.Mixed (GitHub hosts go through Go's
	// default transport; see internal/slack/tlsfp).
	Transport http.RoundTripper
	// Hosts the download (and every redirect) may use, https only.
	// Default GitHubHosts.
	Hosts []string
	// LoopbackHTTP also allows plain http to 127.0.0.1/localhost/::1
	// (CLACK_UPDATE_URL testing against a local fake server).
	LoopbackHTTP bool
	// MaxSize caps a package (default 400 MB); a release that declares a
	// larger one is refused, and a body that runs past the declared size
	// is cut off.
	MaxSize int64
	// Attempts per download, each resuming where the last stopped
	// (default 5), Backoff the first pause between them (doubling).
	Attempts int
	Backoff  time.Duration
}

const (
	defaultMaxSize = 400 << 20
	maxSumsSize    = 1 << 20
)

func (u *Updater) client() *http.Client {
	tr := u.Transport
	if tr == nil {
		tr = tlsfp.Mixed
	}
	return &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("update: too many redirects")
		}
		return u.checkURL(req.URL)
	}}
}

// checkURL refuses anything but https to an allowed host.
func (u *Updater) checkURL(p *url.URL) error {
	host := strings.ToLower(p.Hostname())
	hosts := u.Hosts
	if hosts == nil {
		hosts = GitHubHosts
	}
	allowed := false
	for _, h := range hosts {
		if host == strings.ToLower(h) {
			allowed = true
		}
	}
	switch {
	case p.Scheme == "https" && allowed:
		return nil
	case p.Scheme == "http" && allowed && u.LoopbackHTTP && isLoopback(host):
		return nil
	}
	return fmt.Errorf("%w: %s://%s", ErrHost, p.Scheme, p.Host)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (u *Updater) maxSize() int64 {
	if u.MaxSize > 0 {
		return u.MaxSize
	}
	return defaultMaxSize
}

// safeName reports whether s is a plain file name (release tags and asset
// names become paths under Dir).
func safeName(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 200 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == '_', c == '+':
		default:
			return false
		}
	}
	return true
}

// Path is where a verified package for tag is kept.
func (u *Updater) Path(tag, name string) string { return filepath.Join(u.Dir, tag, name) }

// Download fetches asset a of release r into Dir/<tag>/<name>, resuming
// a partial download, and returns its path once it matches the release's
// SHA256SUMS. A package without a checksum entry, or with a wrong one, is
// never kept.
func (u *Updater) Download(ctx context.Context, r Release, a Asset) (Package, error) {
	path, sum, err := u.download(ctx, r, a)
	if err != nil {
		return Package{}, err
	}
	return Package{Tag: r.Tag, Name: a.Name, Path: path, SHA256: sum}, nil
}

func (u *Updater) download(ctx context.Context, r Release, a Asset) (string, string, error) {
	if !safeName(r.Tag) || !safeName(a.Name) {
		return "", "", fmt.Errorf("update: odd release or file name %q/%q", r.Tag, a.Name)
	}
	if a.Size > u.maxSize() {
		return "", "", fmt.Errorf("%w: %s is %d bytes", ErrTooLarge, a.Name, a.Size)
	}
	var sums Asset
	for _, x := range r.Assets {
		if x.Name == "SHA256SUMS" {
			sums = x
		}
	}
	if sums.URL == "" {
		return "", "", fmt.Errorf("%w (release %s has no SHA256SUMS)", ErrNoChecksum, r.Tag)
	}
	sumsBody, err := u.fetchSmall(ctx, sums.URL, maxSumsSize)
	if err != nil {
		return "", "", fmt.Errorf("update: SHA256SUMS: %w", err)
	}
	want, ok := ParseSums(sumsBody, a.Name)
	if !ok {
		return "", "", ErrNoChecksum
	}
	dir := filepath.Join(u.Dir, r.Tag)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	u.prune(r.Tag)
	dst := filepath.Join(dir, a.Name)
	if VerifyFile(dst, want) == nil {
		return dst, want, nil // downloaded before
	}
	_ = os.Remove(dst)
	part := dst + ".part"
	limit := u.maxSize()
	if a.Size > 0 {
		limit = a.Size
	}
	attempts, backoff := u.Attempts, u.Backoff
	if attempts <= 0 {
		attempts = 5
	}
	if backoff <= 0 {
		backoff = 2 * time.Second
	}
	for i := 0; ; i++ {
		err = u.fetchTo(ctx, a.URL, part, limit, a.Size > 0)
		if err == nil {
			break
		}
		var perm permanent
		if errors.As(err, &perm) || errors.Is(err, ErrTooLarge) || errors.Is(err, ErrHost) || ctx.Err() != nil || i+1 >= attempts {
			if errors.Is(err, ErrTooLarge) {
				_ = os.Remove(part)
			}
			return "", "", err
		}
		log.Printf("update: download %s (try %d): %v; retrying", a.Name, i+1, err)
		select {
		case <-time.After(backoff << i):
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}
	if err := VerifyFile(part, want); err != nil {
		_ = os.Remove(part)
		return "", "", err
	}
	if err := os.Rename(part, dst); err != nil {
		return "", "", err
	}
	return dst, want, nil
}

// permanent marks an error retrying won't fix (a 404, a 403).
type permanent struct{ error }

func (p permanent) Unwrap() error { return p.error }

func (u *Updater) get(ctx context.Context, raw string, offset int64) (*http.Response, error) {
	p, err := url.Parse(raw)
	if err != nil {
		return nil, permanent{err}
	}
	if err := u.checkURL(p); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, permanent{err}
	}
	req.Header.Set("User-Agent", u.UserAgent)
	req.Header.Set("Accept", "application/octet-stream")
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	return u.client().Do(req)
}

func (u *Updater) fetchSmall(ctx context.Context, raw string, max int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	resp, err := u.get(ctx, raw, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	return b, nil
}

// fetchTo downloads raw into part, resuming from part's size. limit is the
// declared size (exact, when exact) or the cap.
func (u *Updater) fetchTo(ctx context.Context, raw, part string, limit int64, exact bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	var offset int64
	if fi, err := os.Stat(part); err == nil {
		offset = fi.Size()
	}
	if offset > limit || (exact && offset == limit) {
		// over the size, or complete but failed a check before: start over
		if err := os.Truncate(part, 0); err != nil {
			return err
		}
		offset = 0
	}
	resp, err := u.get(ctx, raw, offset)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	flags := os.O_WRONLY | os.O_CREATE
	switch {
	case resp.StatusCode == http.StatusPartialContent && offset > 0:
		start, ok := contentRangeStart(resp.Header.Get("Content-Range"))
		if !ok || start != offset {
			_ = os.Truncate(part, 0)
			return fmt.Errorf("update: server resumed at %q, wanted %d", resp.Header.Get("Content-Range"), offset)
		}
		flags |= os.O_APPEND
	case resp.StatusCode == http.StatusOK:
		flags |= os.O_TRUNC // no range support: from the start
		offset = 0
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		_ = os.Truncate(part, 0)
		return errors.New("update: range not satisfiable; restarting")
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("update: %s", resp.Status)
	default:
		return permanent{fmt.Errorf("update: %s", resp.Status)}
	}
	if resp.ContentLength > 0 && offset+resp.ContentLength > limit {
		return fmt.Errorf("%w: %d bytes", ErrTooLarge, offset+resp.ContentLength)
	}
	f, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, limit-offset+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	total := offset + n
	switch {
	case total > limit:
		return fmt.Errorf("%w: more than %d bytes", ErrTooLarge, limit)
	case err != nil:
		return err // keep what arrived; the next try resumes
	case exact && total != limit:
		return fmt.Errorf("update: got %d of %d bytes", total, limit)
	}
	return nil
}

func contentRangeStart(h string) (int64, bool) {
	// "bytes 100-999/1000"
	rest, ok := strings.CutPrefix(h, "bytes ")
	if !ok {
		return 0, false
	}
	i := strings.IndexByte(rest, '-')
	if i < 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(rest[:i], 10, 64)
	return n, err == nil
}

// prune removes downloads of other releases.
func (u *Updater) prune(keep string) {
	ents, err := os.ReadDir(u.Dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() && e.Name() != keep && safeName(e.Name()) && strings.HasPrefix(e.Name(), "v") {
			_ = os.RemoveAll(filepath.Join(u.Dir, e.Name()))
		}
	}
}

// ParseSums finds name's SHA-256 in a sha256sum listing ("<hex>  name" or
// "<hex> *name").
func ParseSums(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 {
			continue
		}
		if strings.TrimPrefix(f[1], "*") != name {
			continue
		}
		h := strings.ToLower(f[0])
		if b, err := hex.DecodeString(h); err != nil || len(b) != sha256.Size {
			return "", false
		}
		return h, true
	}
	return "", false
}

// VerifyFile checks path's SHA-256 against want (hex).
func VerifyFile(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(want) {
		return fmt.Errorf("%w (%s: got %s, want %s)", ErrChecksum, filepath.Base(path), got, want)
	}
	return nil
}
