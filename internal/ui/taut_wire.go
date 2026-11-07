package ui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/messages"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// Taut returns the plugin system (nil until boot, or when it couldn't
// start). Feature packages find it through this optional Host method; every
// taut call is nil-safe and costs one atomic load when no plugin runs.
func (a *App) Taut() *taut.Taut {
	if a.data == nil {
		return nil
	}
	return a.data.taut
}

// startTaut connects Taut plugins (internal/taut) to the native UI
// through slackc. taut.New reads the plugin config only; no JS engine
// starts unless a plugin is enabled.
func (b *backend) startTaut(a *App) {
	if b.core == nil || b.taut != nil {
		return
	}
	t0 := time.Now()
	t, err := taut.New(taut.Options{
		App:            b.core,
		OpenURL:        openURL,
		ClipboardWrite: func(s string) error { a.Post(func() { a.copyText(s) }); return nil },
		ClipboardRead:  func() (string, error) { return "", errors.New("clipboard reads aren't supported") },
		PickFiles:      a.pickFiles,
		UI: taut.UIHooks{
			// Called for the first runtime and again for a fresh one after
			// a crash: drop every mounted root (their JS instances are
			// gone), bind the new bridge, and let views remount (tautGen).
			Attached: func(br *taut.Bridge) {
				a.Post(func() {
					slackc.Reset()
					slackc.SetBridge(br)
					a.tautGen++
					a.Invalidate()
				})
			},
			Deliver: func(root uint64, tree []byte) {
				if err := slackc.Deliver(root, tree); err != nil {
					log.Printf("slackc: deliver %d: %v", root, err)
				}
			},
			SetPatched:  slackc.SetPatched,
			ThreadPane:  messages.ThreadPane, // api.threads.usePane (safe off the UI goroutine)
			CallGo:      slackc.CallGo,
			SetStyle:    slackc.SetStyle,
			RemoveStyle: slackc.RemoveStyle,
			Overlays: func(o []taut.Overlay) {
				roots := make([]uint64, len(o))
				for i, x := range o {
					roots[i] = x.Root
				}
				slackc.KeepOverlays(roots)
			},
			PreferencesTabs: func(tabs []taut.PreferencesTab) {
				a.Post(func() { a.tautTabs = tabs; a.Invalidate() })
			},
			Invalidate: a.Invalidate,
			Toast: func(text, icon string, d time.Duration) {
				a.Post(func() { a.Toast(text) })
			},
			OpenPreferences: func(tab string) {
				a.Post(func() { a.showPrefs(tab) })
			},
			SwitchAccount: func(c slack.Creds) {
				a.Post(func() { a.switchAccount(c) })
			},
			OnDocument: func(body []byte, css []string) {
				if err := slackc.SetDocument(body, css); err != nil {
					log.Printf("slackc: document: %v", err)
				}
			},
			OnDocumentProps: func(html, body []byte) {
				if err := slackc.SetDocumentProps(html, body); err != nil {
					log.Printf("slackc: document props: %v", err)
				}
			},
		},
	})
	if err != nil {
		log.Printf("taut: %v", err)
		return
	}
	b.taut = t
	popups.SetTaut(t)   // channel/member menus, search, reactions, delete
	messages.SetTaut(t) // message list thunks, patched message views
	composer.SetTaut(t) // composer buttons, send checks/transforms, drafts
	if envFlag("TIMING") {
		log.Printf("timing: taut.New %v (js runtime started: %v)", time.Since(t0).Round(10*time.Microsecond), t.Started())
	}
	go b.watchTaut(a, t)
}

// watchTaut repaints when the plugin model changes (Preferences > Taut).
func (b *backend) watchTaut(a *App, t *taut.Taut) {
	ch, stop := t.Subscribe()
	defer stop()
	for range ch {
		a.Invalidate()
	}
}

func (b *backend) stopTaut() {
	if b.taut != nil {
		popups.SetTaut(nil)
		messages.SetTaut(nil)
		composer.SetTaut(nil)
		_ = b.taut.Close()
		b.taut = nil
		slackc.SetBridge(nil)
		slackc.Reset()
	}
}

// SetSwitchAccount sets how api.accounts signs in as other credentials
// (cmd/clack: save creds.json and reconnect).
func (a *App) SetSwitchAccount(f func(c slack.Creds)) { a.opts.SwitchAccount = f }

// switchAccount handles api.accounts: empty creds show the sign-in screen,
// otherwise Clack saves them and reconnects as that account.
func (a *App) switchAccount(c slack.Creds) {
	if c.Token == "" || a.opts.SwitchAccount == nil {
		a.showLogin()
		return
	}
	if a.data != nil {
		a.data.stop()
		a.data = nil
	}
	a.views = map[Route]View{}
	a.viewOrder = nil
	a.convos, a.convoOrder = nil, nil
	a.opts.SwitchAccount(c)
}

// sessionEnded shows the sign-in screen after core reported auth_failed
// (core.App.SignedOut is true from then on); credentials stay on disk so a
// fresh paste replaces them.
func (a *App) sessionEnded() {
	if a.login != nil {
		return
	}
	a.showLogin()
	a.login.err = "Your Slack session ended. Sign in again."
}

// SignedOut reports whether the Slack session ended (sign-in needed).
func (a *App) SignedOut() bool {
	if a.login != nil {
		return true
	}
	c := a.Core()
	return c != nil && c.SignedOut()
}

// showLogin returns to the sign-in screen without deleting credentials.
func (a *App) showLogin() {
	if a.data != nil {
		a.data.stop()
		a.data = nil
	}
	a.views = map[Route]View{}
	a.viewOrder = nil
	a.convos, a.convoOrder = nil, nil
	a.login = newLoginScreen()
	a.Invalidate()
}

// pickFiles backs plugins' <input type=file> with the Gio explorer (the
// composer's attachment chooser). jsrt calls it off the UI goroutine.
func (a *App) pickFiles(accept string, multiple bool) ([]jsrt.PickedFile, error) {
	e := a.explorer
	if e == nil {
		return nil, errors.New("no file chooser on this platform")
	}
	exts := acceptExtensions(accept)
	var rcs []io.ReadCloser
	if multiple {
		r, err := e.ChooseFiles(exts...)
		if err != nil {
			return nil, nil // cancelled (or no chooser): no files
		}
		rcs = r
	} else {
		r, err := e.ChooseFile(exts...)
		if err != nil {
			return nil, nil
		}
		rcs = []io.ReadCloser{r}
	}
	out := make([]jsrt.PickedFile, 0, len(rcs))
	for _, rc := range rcs {
		data, err := io.ReadAll(io.LimitReader(rc, 256<<20))
		name := "file"
		mod := time.Now()
		if f, ok := rc.(interface{ Name() string }); ok {
			name = filepath.Base(f.Name())
			if fi, err := os.Stat(f.Name()); err == nil {
				mod = fi.ModTime()
			}
		}
		rc.Close()
		if err != nil {
			continue
		}
		out = append(out, jsrt.PickedFile{Name: name, Type: mime.TypeByExtension(filepath.Ext(name)), Data: data, LastModified: mod})
	}
	return out, nil
}

// acceptExtensions turns an <input accept> list into the explorer's
// extension filter (".js,.mjs" → [".js" ".mjs"]). MIME patterns such as
// audio/* aren't filterable there, so they mean no filter.
func acceptExtensions(accept string) []string {
	var exts []string
	for _, p := range strings.Split(accept, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, ".") {
			exts = append(exts, p)
		} else if p != "" {
			return nil
		}
	}
	return exts
}

// dispatch runs a Slack thunk through Taut (plugins can wrap it) or the
// native directly. Call off the UI goroutine; natives that touch UI state
// must Post.
func (a *App) dispatch(name string, args map[string]any, native func(ctx context.Context, args map[string]any) (any, error)) {
	t := a.Taut()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := taut.Dispatch(ctx, t, name, args, native); err != nil {
			log.Printf("ui: %s: %v", name, err)
		}
	}()
}

// argStr reads a string thunk argument.
func argStr(args map[string]any, k string) string {
	switch v := args[k].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	}
	return ""
}
