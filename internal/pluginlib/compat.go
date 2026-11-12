package pluginlib

// How far each plugin works in Clack, as docs/PLUGINS-live-slick.md and
// docs/PLUGINS-live-taut.md record it: their ❌ rows (no visible effect in
// Clack) are Broken, their ⚠️ rows (works, with something missing)
// Partial; every other plugin works. TestCompatMatchesDocs keeps this
// table and the docs in step. The line is what Preferences > Plugins, the
// plugin library and the search suggestions show under the plugin's name:
// one short, factual line.

// CompatStatus is a plugin's state in Clack.
type CompatStatus int

const (
	CompatOK      CompatStatus = iota // works as in its own system
	CompatPartial                     // works, with something missing
	CompatBroken                      // no visible effect in Clack
)

// Compat is a plugin's status and its line ("" for CompatOK).
type Compat struct {
	Status CompatStatus
	Line   string
}

func broken(why string) Compat   { return Compat{CompatBroken, "Doesn't work in Clack: " + why} }
func partial(what string) Compat { return Compat{CompatPartial, "Partly works: " + what} }

// compat is keyed system + "/" + id.
var compat = map[string]Compat{
	// docs/PLUGINS-live-slick.md
	Slick + "/AccountSwitcher":      partial("switching accounts goes through Clack's sign-in"),
	Slick + "/AnonymiseFileNames":   partial("renames uploads, but not in the edit-file dialog"),
	Slick + "/BetterHuddles":        broken("needs browser WebRTC"),
	Slick + "/ClearURLs":            partial("cleans links when you send, not when you paste"),
	Slick + "/Click2Load":           broken("Clack shows no iframes for it to hold back"),
	Slick + "/CustomFonts":          broken("Clack can't load web fonts"),
	Slick + "/CustomNameRecording":  partial("can't decode audio, so nothing uploads"),
	Slick + "/CustomSounds":         broken("plugins can't play sounds in Clack"),
	Slick + "/HuddleMuteDetector":   broken("Clack's huddles don't send it the mute alert"),
	Slick + "/InstalledApps":        partial("the app marketplace page asks you to sign in"),
	Slick + "/LetMeSee":             partial("Hide drawings does nothing"),
	Slick + "/oneko":                partial("the cat runs but isn't drawn"),
	Slick + "/OnlineCount":          broken("needs Slack's presence query"),
	Slick + "/PrivateChannelMapper": partial("the missing-channel pill never shows"),
	Slick + "/QuietSpotify":         broken("Clack has no Spotify players to quiet"),
	Slick + "/ShowRealUser":         partial("forwarded copies aren't matched"),
	// docs/PLUGINS-live-taut.md
	Taut + "/BrowserPopouts":      broken("needs pop-out windows"),
	Taut + "/ClickToLoad":         partial("Load shows Clack's media box, not a player"),
	Taut + "/CustomNameRecording": broken("can't decode audio"),
	Taut + "/HuddleActivity":      broken("Clack's huddles keep no event log for it"),
	Taut + "/PrivateChannel":      partial("unknown channel mentions aren't looked up"),
	Taut + "/SlackFixes":          partial("two of its fixes have nothing to fix in Clack"),
	Taut + "/StreamerMode":        partial("doesn't hide VIP badges or screen shares"),
}

// CompatOf is system/id's status in Clack (CompatOK with no line for a
// plugin the docs don't mark).
func CompatOf(system, id string) Compat { return compat[system+"/"+id] }
