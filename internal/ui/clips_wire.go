package ui

import (
	vclip "github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/huddleui"
)

// The composer's audio clip recorder plays Slack's start sound (the
// microphonestart handler's play of cfKp uY) from Slack's CDN through the
// huddle sound player, and names the app where Slack's strings say Slack.
func init() {
	composer.ClipStartSound = func() { huddleui.PlayCDNSound(vclip.StartSound) }
	composer.AppName = AppName
}
