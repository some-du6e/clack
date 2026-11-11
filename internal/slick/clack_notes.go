package slick

// clackNotes say in Preferences what a plugin, or one of its settings,
// does in Clack where Slack's effect doesn't carry over one to one: the
// Clack knob it works instead, or why it can't change anything. They are
// appended to the description Slick's settings UI shows (Settings() →
// PluginInfo.Description, OptionInfo.Description). Key "" is the plugin.
var clackNotes = map[string]map[string]string{
	"Snappy": {
		"":                        "In Clack: the transitions Clack draws (the thread pane's read lines dimming, the channel join banner sliding away, agent plan and task blocks opening) happen at once.",
		"optimizeStateTracking":   "In Clack this does nothing: it opts Slack's own Redux code into two fast paths (the redux_shared_state_descriptors and hot_path_stable_shapes toggles) as that code loads, and Clack's views are native and never run it.",
		"optimizeDuplicateStyles": "In Clack this does nothing: Clack's page has one copy of Slack's stylesheet, so there are no duplicates to skip.",
		"optimizeResize":          "In Clack: plugins' resize listeners and ResizeObservers wait until the drag ends, then run once. Clack's own layout is native and keeps up with the window.",
		"optimizeSelectors":       "In Clack this does nothing: Clack's style engine already indexes those :is() selectors under each class, which is what the rewrite gets Chromium.",
		"ignoreGpuBlocklist":      "In Clack: on Linux with X11 the window tries Vulkan before OpenGL ES (Gio never uses Vulkan on X11, because of NVIDIA driver bugs). Wayland, macOS and Windows have no blocklist, so nothing changes there.",
		"disableCrashReporter":    "In Clack: no crash.log, so a crash's trace isn't copied into Clack's log at the next start.",
		"disableSpellcheck":       "In Clack: Clack's composers stop spellchecking, whatever the spellcheck preference says.",
	},
}

// withClackNote appends a plugin's (key "") or setting's Clack note.
func withClackNote(desc, id, key string) string {
	n := clackNotes[id][key]
	switch {
	case n == "":
		return desc
	case desc == "":
		return n
	}
	return desc + " " + n
}
