package ui

import "github.com/some-du6e/yoloslackgoclient/internal/pluginlib"

// One category taxonomy for Taut's and Slick's plugins. Clack shows the two
// systems as one plugin list, so a category the two name differently
// (Taut's messageBox, Slick's composer) is one group. Preferences >
// Plugins (Installed and Browse) groups and orders plugins by it.

// pluginCategory is one shared group: its key, label and Slack icon.
type pluginCategory struct{ Key, Label, Icon string }

// pluginCategories are the shared groups in display order; "other" is last
// and takes every key not mapped below.
var pluginCategories = []pluginCategory{
	{"messages", "Messages", "message"},
	{"messageBox", "Message box", "compose"},
	{"people", "People", "user"},
	{"channels", "Channels & sidebar", "channel"},
	{"sounds", "Huddles & sounds", "headphones"},
	{"privacy", "Privacy", "lock"},
	{"appearance", "Look & feel", "paintbrush"},
	{"app", "App", "settings"},
	{"fun", "Fun", "emoji"},
	{"hackclub", "Hack Club", "hackclub"},
	{"other", "Other", "code"},
}

// pluginCategoryKeys maps each system's own category keys (Taut's
// PLUGIN_CATEGORIES in shared/Plugin.ts, Slick's in src/shared/settings.ts,
// and the keys the plugin library's index uses) to the shared ones.
var pluginCategoryKeys = map[string]map[string]string{
	pluginlib.Taut: {
		"messages": "messages", "messageBox": "messageBox", "people": "people", "privacy": "privacy",
		"app": "app", "fun": "fun", "other": "other",
	},
	pluginlib.Slick: {
		"messages": "messages", "composer": "messageBox", "people": "people", "channels": "channels",
		"sounds": "sounds", "privacy": "privacy", "appearance": "appearance", "app": "app",
		"hackclub": "hackclub", "other": "other",
	},
}

// sharedCategory is the shared group of a system's category key (unknown
// and empty keys are Other).
func sharedCategory(system, key string) pluginCategory {
	k := pluginCategoryKeys[system][key]
	for _, c := range pluginCategories {
		if c.Key == k {
			return c
		}
	}
	return pluginCategories[len(pluginCategories)-1]
}

// categoryRank is a shared group's place in the list.
func categoryRank(key string) int {
	for i, c := range pluginCategories {
		if c.Key == key {
			return i
		}
	}
	return len(pluginCategories)
}
