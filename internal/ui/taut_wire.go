package ui

import (
	"log"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// startTaut connects Taut plugins (internal/taut) to the native UI
// through slackc. taut.New reads the plugin config only; no JS engine
// starts unless a plugin is enabled.
func (b *backend) startTaut(a *App) {
	if b.core == nil || b.taut != nil {
		return
	}
	t, err := taut.New(taut.Options{
		App:     b.core,
		OpenURL: openURL,
		UI: taut.UIHooks{
			Attached: func(br *taut.Bridge) { slackc.SetBridge(br) },
			Deliver: func(root uint64, tree []byte) {
				if err := slackc.Deliver(root, tree); err != nil {
					log.Printf("slackc: deliver %d: %v", root, err)
				}
			},
			SetPatched:  slackc.SetPatched,
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
			Invalidate: a.Invalidate,
			Toast: func(text, icon string, d time.Duration) {
				a.Post(func() { a.Toast(text) })
			},
			OpenPreferences: func(tab string) {
				a.Post(func() { a.openPrefs() })
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
}

func (b *backend) stopTaut() {
	if b.taut != nil {
		_ = b.taut.Close()
		b.taut = nil
		slackc.SetBridge(nil)
		slackc.Reset()
	}
}
