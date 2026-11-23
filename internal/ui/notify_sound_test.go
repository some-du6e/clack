package ui

// The message sound as Slack's client plays it (client-boot Fpyc
// canPlaySound, boot-shared Eley maybePlayNewMsgSound; docs/NOTIFY-RULES.md
// §7): none while the viewer is in a huddle, and under
// message_sound_debounce one per thread or conversation until 5 s pass
// without another. Synthetic notes only.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// In a huddle a message's notification shows and plays no sound
// (getActiveHuddleCallId); out of it, the sound plays. Clack played it
// over the huddle.
func TestNoMessageSoundInAHuddle(t *testing.T) {
	a, ca, _ := fakePrefsApp(t)
	var snd soundLog
	snd.hook(t)
	rec := &recNotifier{}
	a.notify = rec
	ca.SetDNDStatus(core.DNDStatus{})
	huddleOn(t, true)
	a.notifyMessage("a synthetic title", "a synthetic body", &desktopNote{Channel: "C0SOUND001", Msg: "1762520300.000100"})
	waitFor(t, a, "the notification in the huddle", func() bool { return rec.count() == 1 })
	time.Sleep(300 * time.Millisecond) // the sound's goroutine had its turn
	if got := snd.got(); len(got) != 0 {
		t.Errorf("a message sound in a huddle: %q", got)
	}
	huddleOn(t, false)
	a.notifyMessage("a synthetic title", "a synthetic body", &desktopNote{Channel: "C0SOUND001", Msg: "1762520300.000200"})
	waitFor(t, a, "the sound out of the huddle", func() bool { return len(snd.got()) == 1 })
}

// message_sound_debounce (shouldSuppressConsecutiveMessageSound): a second
// message in the same conversation within the quiet stretch plays nothing
// and starts the stretch again, a reply in a thread has its own, and once
// the stretch passes the conversation plays again. Clack played every one.
func TestMessageSoundDebounceLikeSlack(t *testing.T) {
	a, ca, _ := fakePrefsApp(t)
	var snd soundLog
	snd.hook(t)
	a.notify = &recNotifier{}
	ca.SetDNDStatus(core.DNDStatus{})
	ca.SetExperimentGroup("message_sound_debounce", "on")
	quietFor(t, 500*time.Millisecond)
	note := func(thread, ts string) *desktopNote {
		return &desktopNote{Channel: "C0SOUND001", ThreadTS: thread, Msg: ts}
	}
	a.notifyMessage("t", "b", note("", "1762520400.000100"))
	waitFor(t, a, "the first sound", func() bool { return len(snd.got()) == 1 })
	a.notifyMessage("t", "b", note("", "1762520400.000200"))
	a.notifyMessage("t", "b", note("1762520000.000100", "1762520400.000300"))
	waitFor(t, a, "the thread's sound", func() bool { return len(snd.got()) >= 2 })
	time.Sleep(200 * time.Millisecond)
	if got := snd.got(); len(got) != 2 {
		t.Errorf("the conversation's second message within the stretch played a sound: %d sounds, want 2", len(got))
	}
	time.Sleep(700 * time.Millisecond) // past the stretch the second message started again
	a.notifyMessage("t", "b", note("", "1762520400.000400"))
	waitFor(t, a, "the sound after the stretch", func() bool { return len(snd.got()) >= 3 })
}
