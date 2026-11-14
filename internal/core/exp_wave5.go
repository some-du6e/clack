package core

// The fifth wave of the account's experiments (branch exp-wave-5, CDN
// build fa938a086569, 2026-11-13; docs/EXPERIMENTS.md "wave 5"), read
// the way Slack's client reads them.

import (
	"context"
	"net/url"
)

// ExpPrivateToPublicVisual is fe_private_to_public_slack_connect_and_visual_updates.
const ExpPrivateToPublicVisual = "fe_private_to_public_slack_connect_and_visual_updates"

// PrivateToPublicVisualUpdates is boot-shared fNVg A
// (isPrivateToPublicSlackConnectVisualUpdatesEnabled): "on" ===
// getGroupForUser. additionalOptionsToShow (Xihp) then offers "Change to
// a public channel" for every private channel, disabled for those who
// can't, and AdminChannelModal says why (getDisabledTextPrivateToPublic).
func (a *App) PrivateToPublicVisualUpdates() bool {
	return a.experiment(ExpPrivateToPublicVisual) == "on"
}

// ConvertToPublic is ConvertChannelToPublicModal's Change to Public
// (oWlM convertChannelToPublicApi, 6ndL conversationsConvertToPublic):
// conversations.convertToPublic {channel} (createApiMethodFetcher drops
// the reason).
func (a *App) ConvertToPublic(ctx context.Context, ch string) error {
	return a.C.Call(ctx, "conversations.convertToPublic", url.Values{"channel": {ch}}, nil)
}

// ExpUnderlineButtonLink is a11y_underline_button_link.
const ExpUnderlineButtonLink = "a11y_underline_button_link"

// UnderlineButtonLinks is client-boot CX15 bF: "on" ===
// getGroupForUser("a11y_underline_button_link") and the underline_links
// pref ("Underline links to websites"). Then the HelpLinks that pass it
// (HideUserConfirmationModal's "preferences", Privacy & visibility's
// hidden people "Learn more", the search modal's "Results feel off?
// Learn more") are always underlined (underlineStyle "underline").
func (a *App) UnderlineButtonLinks() bool {
	return a.experiment(ExpUnderlineButtonLink) == "on" && a.Prefs().UnderlineLinks
}

// wave5Reads join experimentReads (TestExperimentReadsListed); their
// defaults are the account's groups (TestDefaultExperimentsMatchAccount).
var wave5Reads = map[string][]string{
	ExpPrivateToPublicVisual: on, ExpUnderlineButtonLink: on,
}

var wave5Defaults = map[string]string{
	ExpPrivateToPublicVisual: "on", ExpUnderlineButtonLink: "on",
}

func init() {
	for name, groups := range wave5Reads {
		experimentReads[name] = groups
	}
	for name, g := range wave5Defaults {
		defaultExperimentGroups[name] = g
	}
}
