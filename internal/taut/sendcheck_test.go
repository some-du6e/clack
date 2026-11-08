package taut

import (
	"context"
	"testing"
	"time"
)

// A send check the runtime's watchdog killed never settles (QuickJS
// leaves an interrupted promise job's promise pending), which held the
// message forever. Once the runtime has interrupted a task, a check that
// stays pending with nothing open for the user is ignored like a check
// that threw; one waiting on a dialog waits as long as the user does.
func TestSendCheckKilledByTheWatchdog(t *testing.T) {
	defer func(g time.Duration) { sendCheckGrace = g }(sendCheckGrace)
	sendCheckGrace = 2 * time.Second
	h := newHarness(t)
	h.js(`
		api.composer.addSendCheck(async (blocks, { channelId }) => {
			if (channelId === 'CDIALOG') {
				const d = document.createElement('dialog')
				document.body.append(d)
				d.showModal()
				return new Promise((resolve) => { globalThis.__answer = (ok) => { d.close(); d.remove(); resolve(ok) } })
			}
			await null
			for (;;) {} // the watchdog interrupts this job
		})
		return 1`)
	eventually(t, 2*time.Second, "send check registered", h.tt.HasSendChecks)
	rt, err := h.tt.ensureRuntime()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	t0 := time.Now()
	if !h.tt.SendCheck(ctx, ComposerContext{Kind: "channel", ChannelID: chGen}) {
		t.Error("a killed check held the message back")
	}
	if d := time.Since(t0); d > 20*time.Second {
		t.Errorf("the killed check was given up after %v", d)
	}
	if rt.rt.Interrupts() == 0 {
		t.Fatal("the watchdog never interrupted the check")
	}

	// waiting for the user (an open modal dialog): never cut short
	res := make(chan bool, 1)
	go func() { res <- h.tt.SendCheck(ctx, ComposerContext{Kind: "channel", ChannelID: "CDIALOG"}) }()
	select {
	case ok := <-res:
		t.Fatalf("a check waiting on its dialog settled by itself (%v)", ok)
	case <-time.After(2*sendCheckGrace + 2*time.Second):
	}
	h.js(`__answer(false); return 1`)
	select {
	case ok := <-res:
		if ok {
			t.Error("the dialog's answer (false) was lost")
		}
	case <-time.After(5 * time.Second):
		t.Error("the check didn't settle after the user answered")
	}
}
