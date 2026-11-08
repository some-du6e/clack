package desktop

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Installing a downloaded update, only ever from the banner's "Install and
// restart" click (ui.Options.InstallUpdate):
//
//  1. Prepare runs while Clack is still open: it unpacks the package next
//     to what it replaces (a .app from the dmg, Clack.exe from the zip),
//     or hands a .deb/.rpm to the system installer behind pkexec's
//     password prompt. It returns a Plan.
//  2. StartHelper copies the running executable to a temp directory and
//     starts it as `clack -apply-update plan.json`; Clack then quits.
//  3. The helper (ApplyUpdate) waits for Clack to exit, swaps the new copy
//     in (or runs the Windows installer silently), and starts Clack again.

// Package is a downloaded, verified update.
type Package struct {
	Tag    string
	Name   string
	Path   string
	SHA256 string // what SHA256SUMS says; checked again before installing
}

// Plan is what the helper does once Clack has exited.
type Plan struct {
	PID int    `json:"pid"`
	Tag string `json:"tag"`
	// Swap renames New over Target (the old one is kept as
	// Target+".clack-old" until the new one is in place).
	Swap *Swap `json:"swap,omitempty"`
	// Run is a command to run and wait for (the Windows installer).
	Run []string `json:"run,omitempty"`
	// Relaunch starts Clack again, whatever happened.
	Relaunch []string `json:"relaunch,omitempty"`
	// Stage is the directory Prepare unpacked into, removed afterwards.
	Stage string `json:"stage,omitempty"`
}

// Swap replaces Target with New.
type Swap struct {
	Target string `json:"target"`
	New    string `json:"new"`
}

// ErrManual: the package was opened in the system's installer (or Finder)
// for the user to finish; Clack keeps running.
var ErrManual = errors.New("update: opened in the system installer")

// ErrCancelled: the user dismissed the password prompt.
var ErrCancelled = errors.New("update: installation cancelled")

// WindowsSetupArgs are the installer flags release.yml's install test uses.
var WindowsSetupArgs = []string{"/VERYSILENT", "/CURRENTUSER", "/SUPPRESSMSGBOXES", "/NORESTART"}

// runCmd runs a command and waits; startCmd starts one detached. Tests
// replace both (and see every command an install would run).
var (
	runCmd = func(ctx context.Context, name string, args ...string) error {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil && len(out) > 0 {
			return fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(string(out)))
		}
		return err
	}
	startCmd = func(name string, args ...string) error {
		c := exec.Command(name, args...)
		detach(c)
		if err := c.Start(); err != nil {
			return err
		}
		return c.Process.Release()
	}
	lookPath = exec.LookPath
)

const stagePrefix = ".clack-update-"

// Prepare gets pkg ready to install over in. ErrManual means it was opened
// in the system installer instead.
func Prepare(ctx context.Context, in Install, pkg Package) (Plan, error) {
	if err := VerifyFile(pkg.Path, pkg.SHA256); err != nil {
		return Plan{}, err
	}
	p := Plan{PID: os.Getpid(), Tag: pkg.Tag}
	switch in.Kind {
	case KindMacApp:
		return prepareMac(ctx, in, pkg, p)
	case KindWinInstaller:
		p.Run = append([]string{pkg.Path}, WindowsSetupArgs...)
		p.Run = append(p.Run, "/LOG="+pkg.Path+".log")
		p.Relaunch = []string{in.Exe}
		return p, nil
	case KindWinPortable:
		stage, err := stageNext(in.Target)
		if err != nil {
			return p, openManual(ctx, pkg.Path, err)
		}
		p.Stage = stage
		dst := filepath.Join(stage, filepath.Base(in.Target))
		if err := unzipFile(pkg.Path, "Clack/Clack.exe", dst, defaultMaxSize); err != nil {
			os.RemoveAll(stage)
			return Plan{}, err
		}
		p.Swap = &Swap{Target: in.Target, New: dst}
		p.Relaunch = []string{in.Target}
		return p, nil
	case KindAppImage:
		stage, err := stageNext(in.Target)
		if err != nil {
			return p, openManual(ctx, filepath.Dir(pkg.Path), err)
		}
		p.Stage = stage
		dst := filepath.Join(stage, filepath.Base(in.Target))
		if err := copyFile(pkg.Path, dst, 0o755); err != nil {
			os.RemoveAll(stage)
			return Plan{}, err
		}
		p.Swap = &Swap{Target: in.Target, New: dst}
		p.Relaunch = []string{in.Target}
		return p, nil
	case KindLinuxBinary:
		// Not from a package: the AppImage goes to ~/Applications.
		home, err := os.UserHomeDir()
		if err != nil {
			return Plan{}, err
		}
		dir := filepath.Join(home, "Applications")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Plan{}, err
		}
		dst := filepath.Join(dir, pkg.Name)
		if err := copyFile(pkg.Path, dst+".tmp", 0o755); err != nil {
			return Plan{}, err
		}
		if err := os.Rename(dst+".tmp", dst); err != nil {
			return Plan{}, err
		}
		p.Relaunch = []string{dst}
		return p, nil
	case KindDeb, KindRPM:
		return prepareLinuxPackage(ctx, in, pkg, p)
	}
	return Plan{}, fmt.Errorf("update: can't install over a %q copy of Clack", in.Kind)
}

// stageNext makes a staging directory beside target (same volume, so the
// swap is a rename).
func stageNext(target string) (string, error) {
	return os.MkdirTemp(filepath.Dir(target), stagePrefix)
}

func prepareMac(ctx context.Context, in Install, pkg Package, p Plan) (Plan, error) {
	if strings.Contains(in.Target, "/AppTranslocation/") {
		// Gatekeeper runs a quarantined app from a read-only copy.
		return p, openManual(ctx, pkg.Path, errors.New("Clack is running from a translocated copy; move Clack.app to Applications"))
	}
	stage, err := stageNext(in.Target)
	if err != nil {
		return p, openManual(ctx, pkg.Path, err)
	}
	p.Stage = stage
	fail := func(err error) (Plan, error) {
		os.RemoveAll(stage)
		return Plan{}, err
	}
	if strings.HasSuffix(pkg.Name, ".dmg") {
		mnt, err := os.MkdirTemp("", "clack-dmg-")
		if err != nil {
			return fail(err)
		}
		defer os.Remove(mnt)
		if err := runCmd(ctx, "hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-noverify", "-mountpoint", mnt, pkg.Path); err != nil {
			return fail(err)
		}
		app, err := findApp(mnt)
		if err == nil {
			err = runCmd(ctx, "ditto", app, filepath.Join(stage, "Clack.app"))
		}
		if derr := runCmd(context.Background(), "hdiutil", "detach", mnt); derr != nil {
			_ = runCmd(context.Background(), "hdiutil", "detach", "-force", mnt)
		}
		if err != nil {
			return fail(err)
		}
	} else {
		if err := runCmd(ctx, "ditto", "-x", "-k", pkg.Path, stage); err != nil {
			return fail(err)
		}
		app, err := findApp(stage)
		if err != nil {
			return fail(err)
		}
		if app != filepath.Join(stage, "Clack.app") {
			if err := os.Rename(app, filepath.Join(stage, "Clack.app")); err != nil {
				return fail(err)
			}
		}
	}
	newApp := filepath.Join(stage, "Clack.app")
	if fi, err := os.Stat(filepath.Join(newApp, "Contents", "MacOS", "clack")); err != nil || !fi.Mode().IsRegular() {
		return fail(fmt.Errorf("update: %s has no Clack.app/Contents/MacOS/clack", pkg.Name))
	}
	p.Swap = &Swap{Target: in.Target, New: newApp}
	p.Relaunch = []string{"/usr/bin/open", in.Target}
	return p, nil
}

// findApp returns the one .app directly in dir.
func findApp(dir string) (string, error) {
	m, _ := filepath.Glob(filepath.Join(dir, "*.app"))
	if len(m) != 1 {
		return "", fmt.Errorf("update: want one .app in the package, found %d", len(m))
	}
	return m[0], nil
}

// prepareLinuxPackage installs a .deb/.rpm with the system's package
// manager behind pkexec, which asks for the user's password (never silent
// root). Without pkexec, or when polkit has no agent, the package opens in
// the desktop's software installer (xdg-open) instead.
func prepareLinuxPackage(ctx context.Context, in Install, pkg Package, p Plan) (Plan, error) {
	var cmd []string
	switch {
	case in.Kind == KindDeb && has("apt-get"):
		cmd = []string{"apt-get", "install", "-y", pkg.Path}
	case in.Kind == KindDeb:
		cmd = []string{"dpkg", "-i", pkg.Path}
	case has("dnf"):
		cmd = []string{"dnf", "install", "-y", pkg.Path}
	case has("zypper"):
		cmd = []string{"zypper", "--non-interactive", "install", "--allow-unsigned-rpm", pkg.Path}
	default:
		cmd = []string{"rpm", "-U", pkg.Path}
	}
	pk, err := lookPath("pkexec")
	if err != nil {
		return p, openManual(ctx, pkg.Path, err)
	}
	if full, err := lookPath(cmd[0]); err == nil {
		cmd[0] = full // pkexec wants an absolute path
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	err = runCmd(ctx, pk, cmd...)
	var exit interface{ ExitCode() int } // *exec.ExitError
	switch {
	case err == nil:
		p.Relaunch = []string{in.Exe}
		return p, nil
	case errors.As(err, &exit) && exit.ExitCode() == 126: // dialog dismissed
		return Plan{}, ErrCancelled
	case errors.As(err, &exit) && exit.ExitCode() == 127: // not authorized, or no polkit agent
		return p, openManual(ctx, pkg.Path, err)
	}
	return Plan{}, err
}

func has(name string) bool { _, err := lookPath(name); return err == nil }

// openManual opens path in the system's handler (Finder for a dmg, the
// software installer for a .deb/.rpm, Explorer for a folder) for the user
// to finish, and returns ErrManual, or why it couldn't.
func openManual(ctx context.Context, path string, why error) error {
	log.Printf("update: installing by hand (%v)", why)
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = runCmd(ctx, "open", path)
	case "windows":
		err = startCmd("explorer.exe", filepath.Dir(path))
	default:
		if !has("xdg-open") {
			return fmt.Errorf("update: %v, and there is no xdg-open to open %s", why, path)
		}
		err = startCmd("xdg-open", path)
	}
	if err != nil {
		return fmt.Errorf("update: %v; opening %s: %w", why, path, err)
	}
	return ErrManual
}

// StartHelper starts a copy of this executable as the update helper for
// plan. The caller quits right after.
func StartHelper(plan Plan) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "clack-update-helper-")
	if err != nil {
		return err
	}
	helper := filepath.Join(dir, "clack-updater")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	if err := copyFile(exe, helper, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(plan, "", "  ")
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, b, 0o600); err != nil {
		return err
	}
	return startCmd(helper, "-apply-update", planPath)
}

// ApplyUpdate is the helper's whole job (`clack -apply-update plan.json`).
// It logs to update.log beside the plan.
func ApplyUpdate(planPath string) error {
	if f, err := os.OpenFile(filepath.Join(filepath.Dir(planPath), "update.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		prev := log.Writer()
		log.SetOutput(io.MultiWriter(os.Stderr, f))
		defer func() { log.SetOutput(prev); f.Close() }()
	}
	b, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	var p Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	err = applyPlan(p, 2*time.Minute)
	if err != nil {
		log.Printf("update: %v", err)
	} else {
		log.Printf("update: installed %s", p.Tag)
	}
	return err
}

var waitExit = waitPID

func applyPlan(p Plan, wait time.Duration) error {
	if p.PID > 0 && !waitExit(p.PID, wait) {
		return fmt.Errorf("update: Clack (pid %d) didn't exit", p.PID)
	}
	err := installPlan(p)
	if p.Stage != "" && strings.HasPrefix(filepath.Base(p.Stage), stagePrefix) {
		os.RemoveAll(p.Stage)
	}
	if len(p.Relaunch) > 0 {
		if rerr := startCmd(p.Relaunch[0], p.Relaunch[1:]...); rerr != nil && err == nil {
			err = fmt.Errorf("update: relaunch: %w", rerr)
		}
	}
	return err
}

func installPlan(p Plan) error {
	if len(p.Run) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := runCmd(ctx, p.Run[0], p.Run[1:]...); err != nil {
			return err
		}
	}
	if s := p.Swap; s != nil {
		if s.Target == "" || s.New == "" {
			return errors.New("update: empty swap")
		}
		old := s.Target + ".clack-old"
		os.RemoveAll(old) // a leftover from an interrupted update
		// Windows may hold the exe a moment after the process exits.
		var err error
		for i := 0; i < 30; i++ {
			if err = os.Rename(s.Target, old); err == nil || os.IsNotExist(err) {
				break
			}
			time.Sleep(time.Second)
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("update: moving the old copy aside: %w", err)
		}
		if err := os.Rename(s.New, s.Target); err != nil {
			_ = os.Rename(old, s.Target)
			return fmt.Errorf("update: putting the new copy in place: %w", err)
		}
		os.RemoveAll(old)
	}
	return nil
}

// CleanHelpers removes update-helper directories left in the temp dir by
// earlier updates (a Windows helper can't delete its own exe).
func CleanHelpers() {
	m, _ := filepath.Glob(filepath.Join(os.TempDir(), "clack-update-helper-*"))
	for _, d := range m {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() && time.Since(fi.ModTime()) > time.Hour {
			os.RemoveAll(d)
		}
	}
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

// unzipFile extracts the entry named name (exactly) from zip src to dst.
func unzipFile(src, name, dst string, max int64) error {
	z, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		if int64(f.UncompressedSize64) > max {
			return ErrTooLarge
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(r, max+1))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil && n > max {
			err = ErrTooLarge
		}
		return err
	}
	return fmt.Errorf("update: %s not in %s", name, filepath.Base(src))
}
