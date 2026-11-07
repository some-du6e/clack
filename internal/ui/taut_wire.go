package ui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/slick"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/blockkit"
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

// deliverTap sees every host tree plugins deliver (tests: what a patched
// boundary drew). Nil outside tests; set before startTaut.
var deliverTap func(root uint64, tree []byte)

// startTaut connects Taut plugins (internal/taut) to the native UI
// through slackc. taut.New reads the plugin config only; no JS engine
// starts unless a plugin is enabled.
func (b *backend) startTaut(a *App) {
	if b.core == nil || b.taut != nil {
		return
	}
	t0 := time.Now()
	// The runtime's patched names, kept so the reset Attached posts can't
	// lose them: a plugin may register its patches (SetPatched) before the
	// UI goroutine runs that reset, and JS only resends the set on change.
	var patchMu sync.Mutex
	var patchedNow []string
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
				patchMu.Lock()
				patchedNow = nil // a fresh runtime starts unpatched
				patchMu.Unlock()
				a.Post(func() {
					slackc.Reset()
					slackc.SetBridge(br)
					patchMu.Lock()
					slackc.SetPatched(patchedNow)
					patchMu.Unlock()
					a.tautGen++
					a.Invalidate()
				})
			},
			Deliver: func(root uint64, tree []byte) {
				if f := deliverTap; f != nil {
					f(root, tree)
				}
				if err := slackc.Deliver(root, tree); err != nil {
					log.Printf("slackc: deliver %d: %v", root, err)
				}
			},
			SetPatched: func(names []string) {
				patchMu.Lock()
				patchedNow = names
				slackc.SetPatched(names)
				patchMu.Unlock()
			},
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
			// slack:// links (Slack's handleDeepLink thunk) open inside Clack.
			DeepLink: a.openDeepLink, // also OpenURL's native (desktop.go)
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
	blockkit.SetTaut(t) // sendActions, executeAppAction, submitView, blocksFormatFetcher
	b.startSlick(a, t)
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
	if b.slick != nil { // Slick runs on Taut's runtime: close it first
		_ = b.slick.Close()
		b.slick = nil
	}
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

// ShowSignIn is messages.SignInHost: the list's "Sign in again" link
// after the session ended opens the sign-in screen.
func (a *App) ShowSignIn() { a.sessionEnded() }

var _ messages.SignInHost = (*App)(nil)

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

// startSlick starts Slick plugins on Taut's runtime. slick.New reads
// settings.json, themes and cached plugin metadata only; with no plugin on,
// no JS starts (themes and custom CSS are converted in Go).
func (b *backend) startSlick(a *App, t *taut.Taut) {
	t0 := time.Now()
	s, err := slick.New(slick.Options{Taut: t, OpenURL: openURL, OpenFile: a.openFilePath})
	if err != nil {
		log.Printf("slick: %v", err)
		return
	}
	b.slick = s
	s.SetDark(a.th.Dark)
	s.SetRoute(b.teamID, a.router.Current().Channel())
	if envFlag("TIMING") {
		log.Printf("timing: slick.New %v (js runtime started: %v)", time.Since(t0).Round(10*time.Microsecond), t.Started())
	}
	go func() { // repaint Preferences > Slick on change
		ch, stop := s.Subscribe()
		defer stop()
		for range ch {
			a.Invalidate()
		}
	}()
}

// Slick returns the Slick plugin system (nil until boot).
func (a *App) Slick() *slick.Slick {
	if a.data == nil {
		return nil
	}
	return a.data.slick
}

// slickRoute tells Slick where the window is (location.pathname,
// getCurrentChannelId, workspace restrictions).
func (a *App) slickRoute(r Route) {
	if s := a.Slick(); s != nil {
		s.SetRoute(a.data.teamID, r.Channel())
	}
}

// openFilePath is Slick's native file picker (file settings,
// MainCtx.dialog.openFile): the chosen file's path, "" when cancelled.
// Blocks; call off the UI goroutine.
func (a *App) openFilePath(title, accept string) (string, error) {
	e := a.explorer
	if e == nil {
		return "", errors.New("no file chooser on this platform")
	}
	rc, err := e.ChooseFile(acceptExtensions(accept)...)
	if err != nil || rc == nil {
		return "", nil // cancelled
	}
	defer rc.Close()
	f, ok := rc.(interface{ Name() string })
	if !ok {
		return "", errors.New("the file chooser didn't return a path")
	}
	return f.Name(), nil
}

// deepLinkRoute maps a slack:// URI to a Clack route (or a user to open):
// slack://channel?team=T&id=C[&thread_ts=ts], slack://user?team=T&id=U,
// slack://open (Home). Unknown forms aren't handled. Deep links only ever
// navigate: nothing here sends, joins, leaves, downloads or signs out, and
// ids and timestamps must look like Slack's so a link can't smuggle extra
// route segments (c/C1/t/… or search?q=…) in.
func deepLinkRoute(uri string) (route, user string, ok bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "slack" {
		return "", "", false
	}
	q := u.Query()
	id := q.Get("id")
	switch u.Host {
	case "channel":
		if !slackIDRE.MatchString(id) || !strings.ContainsAny(id[:1], "CDG") {
			return "", "", false
		}
		r := "c/" + id
		if ts := q.Get("thread_ts"); ts != "" {
			if !slackTSRE.MatchString(ts) {
				return "", "", false
			}
			r += "/t/" + ts // message=… alone opens the channel (no jump-to route yet)
		}
		return r, "", true
	case "user":
		if !slackIDRE.MatchString(id) || !strings.ContainsAny(id[:1], "UW") {
			return "", "", false
		}
		return "", id, true
	case "open":
		return "home", "", true
	}
	return "", "", false
}

var (
	slackIDRE = regexp.MustCompile(`^[A-Z][A-Z0-9]{2,24}$`)
	slackTSRE = regexp.MustCompile(`^[0-9]{1,12}\.[0-9]{1,8}$`)
)
