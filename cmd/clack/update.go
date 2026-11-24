package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
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

	mu  sync.Mutex
	pkg map[string]desktop.Package // verified downloads, by tag
}

func newUpdates(dir string) *updates {
	u := &updates{
		inst: desktop.DetectInstall(),
		dl:   &desktop.Updater{Dir: dir, UserAgent: "clack/" + updateVersion()},
		pkg:  map[string]desktop.Package{},
	}
	if raw := os.Getenv("CLACK_UPDATE_URL"); raw != "" {
		if p, err := url.Parse(raw); err == nil && p.Hostname() != "" {
			// a test server may serve the packages too (plain http only on loopback)
			u.dl.Hosts = append(append([]string(nil), desktop.GitHubHosts...), p.Hostname())
			u.dl.LoopbackHTTP = true
		}
	}
	desktop.CleanHelpers()
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
	return nil
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
	plan, err := desktop.Prepare(context.Background(), u.inst, pkg)
	if errors.Is(err, desktop.ErrManual) {
		return fmt.Errorf("%w: %v", ui.ErrUpdateManual, err)
	}
	if err != nil {
		return err
	}
	_, err = desktop.StartHelper(plan)
	return err
}

// installOnQuit installs the downloaded info as Clack quits, without
// starting it again: Slack desktop's update is applied when the app quits
// (Squirrel installs a downloaded update on quit; its quitAndInstall is
// the restart button's), so the next launch runs it. Only installs that
// swap in silently: never the system installer or a password prompt
// (a .deb or .rpm waits for Install and restart).
func (u *updates) installOnQuit(info ui.UpdateInfo) error {
	switch u.inst.Kind {
	case desktop.KindMacApp, desktop.KindWinInstaller, desktop.KindWinPortable, desktop.KindAppImage:
	default:
		return nil
	}
	u.mu.Lock()
	pkg, ok := u.pkg[info.Tag]
	u.mu.Unlock()
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	plan, err := desktop.Prepare(ctx, u.inst, pkg)
	if errors.Is(err, desktop.ErrManual) {
		return nil // it opened for the user to finish, as Install and restart's does
	}
	if err != nil {
		return err
	}
	plan.Relaunch = nil // quitting: the user starts Clack again
	_, err = desktop.StartHelper(plan)
	return err
}
