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
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
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

// styleTap and documentTap see plugins' stylesheets (key, css; "" when
// removed) and document bodies (tests: the plugin bench). Nil outside tests.
var (
	styleTap    func(key, css string)
	documentTap func(body []byte)
)

// tautOwner is the bridge of the runtime the UI attached last: Attached's
// post binds it to slackc and detachTaut clears it. Only the owner's
// patched names reach slackc, so the patches a detached runtime publishes
// as its plugins stop (sign-out, an account switch) can't leave the next
// session's components on the patched path, its message menus holding for
// a tree that never comes. It isn't read off slackc's bridge: whoever
// wraps that bridge (the plugin bench, the perf plugins job and the
// plugin-gap tests count calls through countingBridge once the runtime is
// up) still talks to this runtime, and its plugins must still patch.
var tautOwner atomic.Pointer[taut.Bridge]

// startTaut connects Taut plugins (internal/taut) to the native UI
// through slackc. taut.New reads the plugin config only; no JS engine
// starts unless a plugin is enabled.
func (b *backend) startTaut(a *App) {
	if b.core == nil || b.taut != nil || a.inert {
		return
	}
	t, s := b.buildTaut(a, a.th.Dark, b.teamID, a.router.Current().Channel(), false)
	b.installTaut(a, t, s)
}

// startTautAsync is startTaut for boot: taut.New and slick.New read the
// plugin config, settings, themes and installed plugins from disk and
// convert the Slick theme's CSS, so they run on a goroutine and only the
// install is posted to the UI goroutine.
func (b *backend) startTautAsync(a *App) {
	if b.core == nil || b.taut != nil || b.tautStarting || a.inert {
		return // inert: the preview renderer never runs plugins (intro_preview_child.go)
	}
	b.tautStarting = true
	team := b.teamID
	if team == "" { // a cold start: boot hasn't answered yet
		team = b.core.C.Creds.Team
	}
	dark, ch := a.th.Dark, a.router.Current().Channel()
	deferJS := b.tautDeferJS // the plugins start later (startDeferredTaut)
	go func() {
		t0 := time.Now()
		t, s := b.buildTaut(a, dark, team, ch, deferJS)
		go func() { // the plugins enabled at start started (reveal.go's slots)
			if t != nil {
				<-t.StartDone()
			}
			if s != nil {
				<-s.StartDone()
			}
			b.pluginsOnce.Do(func() { close(b.pluginsReady) })
		}()
		a.Post(func() {
			b.tautStarting = false
			detail := "none"
			if t != nil && t.Started() {
				detail = "runtime in " + time.Since(t0).Round(time.Millisecond).String()
			}
			a.startupMark("plugins", detail)
			if a.data != b || b.taut != nil || b.cancel == nil || b.ctx.Err() != nil {
				go closeTaut(s, t) // stopped (sign-out, quit) meanwhile
				return
			}
			b.installTaut(a, t, s)
			if deferJS && !b.tautDeferJS {
				startRuntimes(t, s) // the launch's first real frame came while they were being made
			}
		})
	}()
}

// inertPluginDir is name inside the preview renderer's own folder ("" for
// a normal App, whose plugin folders are Taut's and Slick's defaults).
// The renderer never starts plugins (startTaut); this keeps it off the
// user's folders even if something did.
func (a *App) inertPluginDir(name string) string {
	if !a.inert || a.opts.ConfigDir == "" {
		return ""
	}
	return filepath.Join(a.opts.ConfigDir, name)
}

// buildTaut makes the plugin runtimes (any goroutine). nil on error.
func (b *backend) buildTaut(a *App, dark bool, team, channel string, deferJS bool) (*taut.Taut, *slick.Slick) {
	t0 := time.Now()
	// The runtime's patched names, kept so the reset Attached posts can't
	// lose them: a plugin may register its patches (SetPatched) before the
	// UI goroutine runs that reset, and JS only resends the set on change.
	var patchMu sync.Mutex
	var patchedNow []string
	// the bridge the UI goroutine bound for this runtime (Attached): its
	// patched names reach slackc only while it's tautOwner
	var bound atomic.Pointer[taut.Bridge]
	t, err := taut.New(taut.Options{
		DeferStart:     deferJS,
		Dir:            a.inertPluginDir("taut"), // "": Taut's own (clackHome)
		CredsPath:      a.inertPluginDir("creds.json"),
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
					bound.Store(br)
					tautOwner.Store(br)
					patchMu.Lock()
					slackc.SetPatched(patchedNow)
					patchMu.Unlock()
					a.tautGen++
					a.Invalidate()
				})
			},
			Deliver: func(root uint64, tree []byte) {
				a.noteDelivery(root) // reveal waits for plugin renders to go quiet
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
				if br := bound.Load(); br != nil && tautOwner.Load() == br {
					slackc.SetPatched(names)
				}
				patchMu.Unlock()
			},
			ThreadPane: messages.ThreadPane, // api.threads.usePane (safe off the UI goroutine)
			CallGo:     slackc.CallGo,
			SetStyle: func(key, src string) {
				if f := styleTap; f != nil {
					f(key, src)
				}
				if key != "user" && !strings.HasPrefix(key, "slick:") {
					b.pluginStyled.Store(true) // a plugin's own CSS, not the user's or Slick's theme (prefs.PluginStyled)
				}
				slackc.SetStyle(key, src)
			},
			RemoveStyle: func(key string) {
				if f := styleTap; f != nil {
					f(key, "")
				}
				slackc.RemoveStyle(key)
			},
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
				if f := documentTap; f != nil {
					f(body)
				}
				if len(css) > 0 {
					b.pluginStyled.Store(true) // a page's style tags (prefs.PluginStyled)
				}
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
		return nil, nil
	}
	if envFlag("TIMING") {
		log.Printf("timing: taut.New %v (js runtime started: %v)", time.Since(t0).Round(10*time.Microsecond), t.Started())
	}
	return t, b.buildSlick(a, t, dark, team, channel, deferJS)
}

// installTaut hooks the runtimes into the UI (UI goroutine).
func (b *backend) installTaut(a *App, t *taut.Taut, s *slick.Slick) {
	if t == nil {
		return
	}
	b.taut = t
	popups.SetTaut(t)   // channel/member menus, search, reactions, delete
	messages.SetTaut(t) // message list thunks, patched message views
	composer.SetTaut(t) // composer buttons, send checks/transforms, drafts
	blockkit.SetTaut(t) // sendActions, executeAppAction, submitView, blocksFormatFetcher
	// @mentions name the member as plugins present it (Nicknames)
	richview.SetMemberLabel(newMentionLabels(t))
	if s != nil {
		b.slick = s
		a.slickSetDark(s, a.th.Dark)     // if the mode changed while it started
		a.slickRoute(a.router.Current()) // likewise the route
		go func() {                      // repaint Preferences > Slick on change
			ch, stop := s.Subscribe()
			defer stop()
			for range ch {
				a.Invalidate()
			}
		}()
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
	s, t := b.slick, b.taut
	b.detachTaut()
	closeTaut(s, t)
}

// detachTaut unhooks the plugin runtime from the UI (UI goroutine).
func (b *backend) detachTaut() {
	b.slick = nil
	if b.taut != nil {
		popups.SetTaut(nil)
		messages.SetTaut(nil)
		composer.SetTaut(nil)
		richview.SetMemberLabel(nil)
		b.taut = nil
		tautOwner.Store(nil)
		slackc.SetBridge(nil)
		slackc.Reset()
	}
}

// closeTaut stops Slick and Taut's runtime (any goroutine; it can take
// seconds: each plugin's stop hook runs).
func closeTaut(s *slick.Slick, t *taut.Taut) {
	if s != nil { // Slick runs on Taut's runtime: close it first
		_ = s.Close()
	}
	if t != nil {
		_ = t.Close()
		composer.ForgetTaut(t) // composer's mark that it extended t
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
	a.closeInspectSession() // its records are the account's views
	a.closeVideos()         // and its videos (video.go)
	if a.data != nil {
		_ = a.data.stop()
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
	a.closeInspectSession() // inspect_wire.go
	a.closeVideos()         // nothing plays on behind the sign-in screen (video.go)
	if a.data != nil {
		_ = a.data.stop()
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

// buildSlick starts Slick plugins on Taut's runtime (any goroutine).
// slick.New reads settings.json, themes and cached plugin metadata only;
// with no plugin on, no JS starts (themes and custom CSS are converted in
// Go).
func (b *backend) buildSlick(a *App, t *taut.Taut, dark bool, team, channel string, deferJS bool) *slick.Slick {
	t0 := time.Now()
	s, err := slick.New(slick.Options{Dir: a.inertPluginDir("slick"), Taut: t, OpenURL: openURL, OpenFile: a.openFilePath,
		SlackCSS: slackc.NativeTransitionsCSS, SlackCSSHref: slackc.SlackCSSHref, SpellChecker: a.setSessionSpellChecker,
		DeferStart: deferJS, Dark: dark})
	if err != nil {
		log.Printf("slick: %v", err)
		return nil
	}
	s.SetDark(dark)
	s.SetRoute(team, channel, "")
	if envFlag("TIMING") {
		log.Printf("timing: slick.New %v (js runtime started: %v)", time.Since(t0).Round(10*time.Microsecond), t.Started())
	}
	return s
}

// slickSetDark tells Slick the mode off the UI goroutine (a mode change
// re-applies the Slick theme, which converts and parses its CSS). Changes
// are applied in order; the latest wins.
func (a *App) slickSetDark(s *slick.Slick, dark bool) {
	if s == nil {
		return
	}
	a.slickMode.mu.Lock()
	a.slickMode.s, a.slickMode.dark = s, dark
	if a.slickMode.busy {
		a.slickMode.mu.Unlock()
		return
	}
	a.slickMode.busy = true
	a.slickMode.mu.Unlock()
	go func() {
		for {
			a.slickMode.mu.Lock()
			s, dark := a.slickMode.s, a.slickMode.dark
			a.slickMode.mu.Unlock()
			s.SetDark(dark)
			a.slickMode.mu.Lock()
			if a.slickMode.s == s && a.slickMode.dark == dark {
				a.slickMode.busy = false
				a.slickMode.mu.Unlock()
				return
			}
			a.slickMode.mu.Unlock()
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
		s.SetRoute(a.data.teamID, r.Channel(), r.Thread())
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

// deepLinkMessage is handleParsedDeepLink's "channel" command with a
// message or a thread (slack://channel?team=T&id=C&message=ts
// [&thread_ts=ts]; boot-render VxbD, build 2c0d02ca130f): jumpToMessage
// ({channelId, ts: message, threadTs: thread_ts}), which finds the
// thread when only the message is named; a thread alone is toThread
// with highlightRoot; message "latest" with a thread is its latest reply
// (cdTA Mz, jumpLatest). Both in formatMessageTimestamp's forms. ok is
// false for any other link, and for an id or a timestamp that doesn't
// look like Slack's (the channel alone goes to deepLinkRoute).
func deepLinkMessage(uri string) (ch, ts, thread string, latest, ok bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "slack" || u.Host != "channel" {
		return "", "", "", false, false
	}
	q := u.Query()
	ch = q.Get("id")
	if !slackIDRE.MatchString(ch) || !strings.ContainsAny(ch[:1], "CDG") {
		return "", "", "", false, false
	}
	m := q.Get("message")
	if m != "" && m != "latest" {
		if ts = slackTimestamp(m); ts == "" {
			return "", "", "", false, false
		}
	}
	if t := q.Get("thread_ts"); t != "" {
		if thread = slackTimestamp(t); thread == "" {
			return "", "", "", false, false
		}
	}
	latest = m == "latest" && thread != ""
	return ch, ts, thread, latest, ts != "" || thread != ""
}

var (
	slackIDRE = regexp.MustCompile(`^[A-Z][A-Z0-9]{2,24}$`)
	slackTSRE = regexp.MustCompile(`^[0-9]{1,12}\.[0-9]{1,8}$`)
)
