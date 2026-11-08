package desktop

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"
)

// CLI is `clack --update`: the escape hatch for a Clack that won't open.
// It runs without a window and without Clack's config (no ui.json, no
// credentials, no account theme), asks GitHub for the newest release the
// way the banner does, downloads the package for this install under the
// same rules (Updater), checks SHA256SUMS, and installs it with the same
// Prepare + helper code as "Install and restart". It never starts Clack
// afterwards: the copy that froze shouldn't pop up from a terminal.
type CLI struct {
	Version string // this build's (CLACK_UPDATE_AS overrides it in cmd/clack)
	Check   bool   // --check: report only
	Yes     bool   // --yes: no y/N prompt
	In      io.Reader
	Out     io.Writer
	Install Install
	Updater *Updater
	// Latest finds the newest release (default LatestRelease: the banner's
	// logic, pre-release aware).
	Latest func(ctx context.Context, version string) (Release, error)
	// Apply installs a prepared plan. It reports whether the work goes on
	// in the background after this process exits (the Windows helper).
	// Default: in this process on macOS and Linux; on Windows, the helper,
	// because Clack.exe can't be replaced (or reinstalled) while it runs.
	Apply func(p Plan) (background bool, logPath string, err error)
}

// ErrNotConfirmed: the user didn't answer y.
var ErrNotConfirmed = errors.New("update: not confirmed (answer y, or pass --yes)")

// ErrDevBuild: a build without a release version can't tell what's newer.
var ErrDevBuild = errors.New("update: this is a development build with no release version to compare (set CLACK_UPDATE_AS=v0.0.0 to update anyway)")

// Run does the whole update and returns nil when Clack is updated (or was
// already up to date, or --check reported).
func (c *CLI) Run(ctx context.Context) error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}
	say := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }
	where := c.Install.Target
	if where == "" {
		where = c.Install.Exe
	}
	say("Clack %s (install type: %s, at %s)", c.Version, kindName(c.Install.Kind), where)
	if !Released(c.Version) {
		return ErrDevBuild
	}
	latest := c.Latest
	if latest == nil {
		latest = LatestRelease
	}
	say("Checking GitHub Releases for a newer version…")
	r, err := latest(ctx, c.Version)
	if err != nil {
		return fmt.Errorf("update: checking for updates: %w", err)
	}
	if !Newer(r.Tag, c.Version) {
		say("Clack is up to date (newest release: %s).", r.Tag)
		return nil
	}
	say("Newer release: %s (%s)", r.Tag, r.URL)
	a, err := c.Install.PickAsset(r)
	if err != nil {
		return fmt.Errorf("%w; download it from %s", err, r.URL)
	}
	say("Package for this install: %s (%s)", a.Name, mb(a.Size))
	if c.Check {
		say("Run `clack --update` to download and install it.")
		return nil
	}
	if !c.Yes {
		fmt.Fprintf(out, "Download and install Clack %s? [y/N] ", r.Tag)
		line, _ := bufio.NewReader(c.in()).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			return ErrNotConfirmed
		}
	}
	u := *c.Updater
	last := time.Time{}
	u.Progress = func(done, total int64) {
		if now := time.Now(); now.Sub(last) >= 500*time.Millisecond || done >= total {
			last = now
			pct := 0
			if total > 0 {
				pct = int(done * 100 / total)
			}
			fmt.Fprintf(out, "\r  downloading %s / %s (%d%%)   ", mb(done), mb(total), pct)
		}
	}
	say("Downloading %s…", a.URL)
	pkg, err := u.Download(ctx, r, a)
	fmt.Fprintln(out)
	if err != nil {
		return err
	}
	say("Verified against SHA256SUMS: %s", pkg.SHA256)
	say("Installing…")
	plan, err := Prepare(ctx, c.Install, pkg)
	if errors.Is(err, ErrManual) {
		say("Opened %s in the system installer: finish there, then start Clack.", pkg.Name)
		return nil
	}
	if err != nil {
		return err
	}
	plan.Relaunch = nil // from a terminal: the user starts Clack themselves
	apply := c.Apply
	if apply == nil {
		apply = defaultApply
	}
	bg, logPath, err := apply(plan)
	if err != nil {
		return err
	}
	if bg {
		say("Clack %s installs as soon as this command exits (log: %s). Then start Clack.", r.Tag, logPath)
		return nil
	}
	if c.Install.Kind == KindLinuxBinary {
		say("Clack %s is in ~/Applications/%s; start it from there.", r.Tag, pkg.Name)
		return nil
	}
	say("Updated to Clack %s. Start Clack again.", r.Tag)
	return nil
}

func (c *CLI) in() io.Reader {
	if c.In != nil {
		return c.In
	}
	return os.Stdin
}

func defaultApply(p Plan) (bool, string, error) {
	if runtime.GOOS == "windows" {
		p.PID = os.Getpid()
		log, err := StartHelper(p)
		return true, log, err
	}
	p.PID = 0 // this process isn't the app being replaced; swap now
	return false, "", applyPlan(p, 0)
}

func kindName(k InstallKind) string {
	if k == KindUnknown {
		return "unknown"
	}
	return string(k)
}

func mb(n int64) string {
	if n <= 0 {
		return "? MB"
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}
