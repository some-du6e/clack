package core

// The sixth wave of the account's experiments (branch exp-wave-6, CDN
// build fa938a086569, 2026-11-13; docs/EXPERIMENTS.md "wave 6"): what
// wave 5 left, read the way Slack's client reads them.

import (
	"context"
	"net/url"
)

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

// Settings' managing-this-channel rows (boot-shared Xihp
// additionalOptionsToShow; views/details_managing.go): the permissions
// permissions.info answers for them.
const (
	PermDeleteChannel              = "DELETE_CHANNEL"                 // Xihp deleteChannel
	PermConvertToPrivate           = "CONVERT_TO_PRIVATE"             // 8gbq M
	PermConvertToPrivateUnderPrefs = "CONVERT_TO_PRIVATE_UNDER_PREFS" // 8gbq M, with CREATE_PRIVATE_CHANNEL
	PermCreatePrivateChannel       = "CREATE_PRIVATE_CHANNEL"         // 2v1U Rt (the team's, no channel)
)

// ConvertToPrivate is ConvertChannelToPrivateModal's Change to Private
// (9ldh convertChannelToPrivateApi, 6ndL conversationsConvertToPrivate):
// conversations.convertToPrivate {channel} (no name or user_ids from
// this modal).
func (a *App) ConvertToPrivate(ctx context.Context, ch string) error {
	return a.C.Call(ctx, "conversations.convertToPrivate", url.Values{"channel": {ch}}, nil)
}

// DeleteChannel is DeleteChannelModal's Delete Channel (ew/6, 6ndL
// setChannelDeleted): conversations.delete {channel}; the channel leaves
// the sidebar.
func (a *App) DeleteChannel(ctx context.Context, ch string) error {
	if err := a.C.Call(ctx, "conversations.delete", url.Values{"channel": {ch}}, nil); err != nil {
		return err
	}
	a.S.Lock()
	delete(a.S.Convs, ch)
	a.S.Unlock()
	a.emit(Event{Type: "conv_removed", Channel: ch})
	return nil
}
