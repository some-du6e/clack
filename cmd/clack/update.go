package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/desktop"
	"github.com/some-du6e/yoloslackgoclient/internal/ui"
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
func (u *updates) check(ctx context.Context) (ui.UpdateInfo, bool, error) {
	cur := updateVersion()
	r, err := desktop.LatestRelease(ctx, cur)
	if err != nil {
		return ui.UpdateInfo{}, false, err
	}
	return ui.UpdateInfo{Tag: r.Tag, URL: r.URL, Release: r}, desktop.Newer(r.Tag, cur), nil
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
