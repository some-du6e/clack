package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/ui"
)

// updates wires the update check, the background download and the
// "Install and restart" click to internal/desktop. CLACK_UPDATE_URL points
// the check (and the downloads) at another releases API for testing;
// CLACK_UPDATE_AS pretends to be another version.
type updates struct {
	inst desktop.Install
	dl   *desktop.Updater

	mu    sync.Mutex
	pkg   map[string]desktop.Package // verified downloads, by tag
	plans map[string]desktop.Plan    // their staged installs (stageForQuit), by tag
}

func newUpdates(dir string) *updates {
	u := &updates{
		inst:  desktop.DetectInstall(),
		dl:    &desktop.Updater{Dir: dir, UserAgent: "clack/" + updateVersion()},
		pkg:   map[string]desktop.Package{},
		plans: map[string]desktop.Plan{},
	}
	if raw := os.Getenv("CLACK_UPDATE_URL"); raw != "" {
		if p, err := url.Parse(raw); err == nil && p.Hostname() != "" {
			// a test server may serve the packages too (plain http only on loopback)
			u.dl.Hosts = append(append([]string(nil), desktop.GitHubHosts...), p.Hostname())
			u.dl.LoopbackHTTP = true
		}
	}
	desktop.CleanHelpers()
	desktop.CleanStages(u.inst.Target)
	log.Printf("update: this copy is a %q install", u.inst.Kind)
	return u
}

func updateVersion() string {
	if v := os.Getenv("CLACK_UPDATE_AS"); v != "" {
		return v
	}
	return version
}

// check asks GitHub Releases whether a newer Clack is out (metadata only).
// A newer release counts once this install's package and SHA256SUMS are
// uploaded; until then it is ui.ErrUpdatePending, which the UI retries
// in minutes without offering (or downloading) it.
func (u *updates) check(ctx context.Context) (ui.UpdateInfo, bool, error) {
	return u.checkAt(ctx, time.Now())
}

func (u *updates) checkAt(ctx context.Context, now time.Time) (ui.UpdateInfo, bool, error) {
	cur := updateVersion()
	r, err := desktop.LatestRelease(ctx, cur)
	if err != nil {
		return ui.UpdateInfo{}, false, err
	}
	info := ui.UpdateInfo{Tag: r.Tag, URL: r.URL, Release: r}
	if !desktop.Newer(r.Tag, cur) {
		return info, false, nil
	}
	if _, err := u.inst.ReadyAsset(r, now); errors.Is(err, desktop.ErrAssetsPending) {
		return info, false, fmt.Errorf("%w: %v", ui.ErrUpdatePending, err)
	}
	// ready, or no package for this install at all (the release page)
	return info, true, nil
}

// download fetches and verifies the package for this install, telling
// progress how far it has got.
func (u *updates) download(ctx context.Context, info ui.UpdateInfo, progress func(ui.UpdateProgress)) error {
	r, ok := info.Release.(desktop.Release)
	if !ok {
		return errors.New("update: no release")
	}
	a, err := u.inst.PickAsset(r)
	if err != nil {
		return err
	}
	dl := *u.dl // a stopped download may still be running: each gets its own Progress
	if progress != nil {
		dl.Progress = func(p desktop.Progress) { progress(uiProgress(p)) }
	}
	pkg, err := dl.Download(ctx, r, a)
	if err != nil {
		return err
	}
	log.Printf("update: %s downloaded and verified (%s)", pkg.Name, pkg.SHA256)
	u.mu.Lock()
	u.pkg[r.Tag] = pkg
	u.mu.Unlock()
	u.stageForQuit(ctx, r.Tag, pkg)
	return nil
}

// quietKinds are the installs that swap in silently, so they may go in
// as Clack quits: never the system installer or a password prompt (a
// .deb or .rpm waits for Install and restart).
func quietKind(k desktop.InstallKind) bool {
	switch k {
	case desktop.KindMacApp, desktop.KindWinInstaller, desktop.KindWinPortable, desktop.KindAppImage:
		return true
	}
	return false
}

// stageForQuit gets a verified download ready to install while Clack
// still runs (the dmg mounted and its app copied beside Clack.app, the
// archive unpacked), so quitting only starts the helper: staging inside
// the quitting process left hdiutil and ditto runs behind when its exit
// came first. A copy Slack would call unable to update itself
// (getIsUpdateSupported), or one that needs the user, isn't staged and
// waits for Install and restart.
func (u *updates) stageForQuit(ctx context.Context, tag string, pkg desktop.Package) {
	if !quietKind(u.inst.Kind) {
		return
	}
	if why := desktop.UpdateUnsupported(u.inst); why != "" {
		log.Printf("update: %s installs only from Install and restart: %s", tag, why)
		return
	}
	plan, err := desktop.PrepareQuiet(ctx, u.inst, pkg)
	if err != nil {
		log.Printf("update: %s not staged to install on quit: %v", tag, err)
		return
	}
	u.mu.Lock()
	if old, ok := u.plans[tag]; ok && old.Stage != "" && old.Stage != plan.Stage {
		os.RemoveAll(old.Stage)
	}
	u.plans[tag] = plan
	u.mu.Unlock()
	log.Printf("update: %s staged to install as Clack quits", tag)
}

// uiProgress maps the downloader's stages onto the UI's.
func uiProgress(p desktop.Progress) ui.UpdateProgress {
	st := ui.UpdateDownloading
	switch p.Stage {
	case desktop.StageVerifying:
		st = ui.UpdateVerifying
	case desktop.StageStaging:
		st = ui.UpdateStaging
	case desktop.StageReady:
		st = ui.UpdateStaged
	}
	return ui.UpdateProgress{Stage: st, Done: p.Done, Total: p.Total}
}

// install is the "Install and restart" click.
func (u *updates) install(info ui.UpdateInfo) error {
	u.mu.Lock()
	pkg, ok := u.pkg[info.Tag]
	u.mu.Unlock()
	if !ok {
		return fmt.Errorf("update: %s isn't downloaded", info.Tag)
	}
	u.mu.Lock()
	plan, staged := u.plans[info.Tag]
	delete(u.plans, info.Tag)
	u.mu.Unlock()
	var err error
	if !staged { // staged for the quit already: the same plan, relaunch and all
		plan, err = desktop.Prepare(context.Background(), u.inst, pkg)
	}
	if errors.Is(err, desktop.ErrManual) {
		return fmt.Errorf("%w: %v", ui.ErrUpdateManual, err)
	}
	if err != nil {
		return err
	}
	_, err = desktop.StartHelper(plan)
	return err
}

// quitInstallBlocked is why a quit with exit reason why installs nothing,
// or nil: a quit as Windows ends the session (desktop.SessionEndMark) may
// have its installer or swap killed halfway, Clack left broken, so the
// update waits for the next quit (or Install and restart). The quit then
// gets 3 s before Windows ends the process; the helper would be ended
// with the session too.
func quitInstallBlocked(why string) error {
	if strings.Contains(why, desktop.SessionEndMark) {
		return fmt.Errorf("%w: Windows is ending the session", errNotOnQuit)
	}
	return nil
}

// errNotOnQuit: installOnQuit ran nothing, and why.
var errNotOnQuit = errors.New("update: not installed on quit")

// installOnQuit installs the downloaded info as Clack quits, without
// starting it again: Slack desktop's update is applied when the app quits
// (Squirrel installs a downloaded update on quit; its quitAndInstall is
// the restart button's), so the next launch runs it. It only starts the
// helper on what stageForQuit staged (quietKind installs); anything else
// is errNotOnQuit with the reason, and waits.
func (u *updates) installOnQuit(info ui.UpdateInfo) error {
	if !quietKind(u.inst.Kind) {
		return fmt.Errorf("%w: a %q install waits for Install and restart", errNotOnQuit, u.inst.Kind)
	}
	u.mu.Lock()
	plan, ok := u.plans[info.Tag]
	delete(u.plans, info.Tag)
	u.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: nothing staged for it", errNotOnQuit)
	}
	plan.Relaunch = nil // quitting: the user starts Clack again
	_, err := desktop.StartHelper(plan)
	return err
}
