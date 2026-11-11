package ui

import "github.com/some-du6e/clack/internal/pluginlib"

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

// tautGroups are Taut's own list groups (app/settings/pluginList.tsx
// pluginGroup): its PLUGIN_CATEGORIES labels in order, then "User plugins"
// for a user plugin in a category Taut doesn't have, then "Other".
// Preferences > Plugins lists both systems' plugins under them, the way
// Taut lists its own: a Slick plugin's category counts where Taut has the
// same one (Slick's composer is Taut's Message Box).
var tautGroups = []pluginCategory{
	{"messages", "Messages", ""}, {"messageBox", "Message Box", ""}, {"people", "People", ""},
	{"privacy", "Privacy", ""}, {"app", "App", ""}, {"fun", "Fun", ""},
	{"user", "User plugins", ""}, {"other", "Other", ""},
}

// tautCategoryKeys maps each system's category keys onto Taut's.
var tautCategoryKeys = map[string]map[string]string{
	pluginlib.Taut: {"messages": "messages", "messageBox": "messageBox", "people": "people", "privacy": "privacy",
		"app": "app", "fun": "fun"},
	pluginlib.Slick: {"messages": "messages", "composer": "messageBox", "people": "people", "privacy": "privacy",
		"app": "app"},
}

// tautGroup is Taut's pluginGroup: the plugin's category, else User
// plugins for a user plugin, else Other.
func tautGroup(system, key string, user bool) pluginCategory {
	k := tautCategoryKeys[system][key]
	switch {
	case k != "":
	case user:
		k = "user"
	default:
		k = "other"
	}
	for _, g := range tautGroups {
		if g.Key == k {
			return g
		}
	}
	return tautGroups[len(tautGroups)-1]
}

// tautGroupRank is a group's place in Taut's list.
func tautGroupRank(key string) int {
	for i, g := range tautGroups {
		if g.Key == key {
			return i
		}
	}
	return len(tautGroups)
}
