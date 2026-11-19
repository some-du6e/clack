//go:build js && wasm

// Command clack-web is the browser demo at getclack.club/demo/: Clack
// itself on Gio's js/wasm target, signed in to Quillfeather Paper Co. (the
// internal/demo workspace) with the fake Slack server running in the same
// page. A page has no sockets, so the fake serves on an in-memory listener
// and every connection Clack opens goes to it (demo.StartInMemory):
// nothing reaches Slack, or any other host. Files live in the page's
// memory (site/demo/fs.js), so a reload starts over.
//
// The page (site/demo/index.html) defines globalThis.clackDemo before the
// program starts; status, ready and failed are called as it boots. With
// clackDemo.probe set (the CI browser test, site/tools/demo-test.mjs) it
// adds drawn(), the strings the last frame drew, lastPost(channel), what
// you last posted there as the fake has it, and general, #general's id.
package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"syscall/js"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// version is the build's, from -ldflags "-X main.version=…".
var version = "dev"

// dir is the demo's settings folder, in the page's memory.
const dir = "/clack-demo"

func main() {
	started := time.Now()
	page := js.Global().Get("clackDemo")
	call := func(name string, args ...any) {
		if page.Truthy() && page.Get(name).Type() == js.TypeFunction {
			page.Call(name, args...)
		}
	}
	fatal := func(err error) {
		call("failed", err.Error())
		log.Fatal(err)
	}
	log.SetOutput(slack.RedactWriter(os.Stderr))
	call("status", "Starting the workspace")
	sess, err := demo.StartInMemory(demo.Options{})
	if err != nil {
		fatal(err)
	}
	cache := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		log.Printf("clack-web: %v", err)
	}
	// #general open and live updates on, as `clack demo` starts
	if err := os.WriteFile(filepath.Join(dir, "ui.json"), []byte(`{"realtime":true,"last_channel":"`+demo.GeneralID+`"}`+"\n"), 0o600); err != nil {
		log.Printf("clack-web: %v", err)
	}

	probe := page.Truthy() && page.Get("probe").Truthy()
	drawn := map[string]bool{}
	if probe {
		page.Set("general", demo.GeneralID)
		theme.DrawHook = func(s string) { drawn[s] = true }
		page.Set("drawn", js.FuncOf(func(js.Value, []js.Value) any {
			out := make([]any, 0, len(drawn))
			for s := range drawn {
				out = append(out, s)
			}
			return out
		}))
		page.Set("lastPost", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) < 1 {
				return nil
			}
			var last *slack.Message
			for _, m := range sess.World.History(args[0].String()) {
				if m.User == demo.SelfID && (last == nil || m.TS > last.TS) {
					m := m
					last = &m
				}
			}
			if last == nil {
				return nil
			}
			return map[string]any{"text": last.Text, "ts": last.TS}
		}))
	}

	images := imgcache.New(sess.Fetch, filepath.Join(cache, "img"), nil)
	ui.SetVersion(version)
	first := true
	a := ui.New(ui.Options{ConfigDir: dir, Started: started,
		Demo:    "Quillfeather Paper Co. and everyone in it are made up. Nothing here connects to Slack.",
		SignOut: func() {},
		OnWindowEvent: func(e event.Event) {
			if _, ok := e.(app.FrameEvent); !ok {
				return
			}
			if probe {
				drawn = map[string]bool{}
			}
			if first {
				first = false
				call("ready")
			}
		},
	}, images)
	c, err := sess.NewClient()
	if err != nil { // the session's own fake is always loopback; never reached
		fatal(err)
	}
	a.Attach(core.New(c, store.New(cache)), cache)
	a.SetLoginHandler(func(slack.SignInInput) error {
		return errors.New("this is the demo workspace: download Clack to sign in to Slack")
	})
	go func() {
		err := a.Run()
		sess.Close()
		if err != nil {
			call("failed", err.Error())
			log.Print(err)
		}
		call("ended")
	}()
	app.Main()
}
