package core

// The sixth wave of the account's experiments (branch exp-wave-6, CDN
// build fa938a086569, 2026-11-13; docs/EXPERIMENTS.md "wave 6"): what
// wave 5 left, read the way Slack's client reads them.

// ExpListsSubTasks is lists_sub_tasks: client-boot Hm0j
// isListsSubTasksEnabled ("on" === getGroupForUser), read by the list
// views (listview/subtasks.go).
const ExpListsSubTasks = "lists_sub_tasks"

// wave6Reads join experimentReads (TestExperimentReadsListed); their
// defaults are the account's groups (TestDefaultExperimentsMatchAccount).
var wave6Reads = map[string][]string{
	ExpListsSubTasks: on,
}

var wave6Defaults = map[string]string{
	ExpListsSubTasks: "on",
}

func init() {
	for name, groups := range wave6Reads {
		experimentReads[name] = groups
	}
	for name, g := range wave6Defaults {
		defaultExperimentGroups[name] = g
	}
}
