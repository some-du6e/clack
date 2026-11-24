package core

// The megaphone boot load's spaces and the answers kept, as Slack's client
// asks for and files them (client-boot 17Qm, build 66e72b57ade1).
// Synthetic data on internal/fakeslack.

import (
	"context"
	"slices"
	"testing"
)

// chunkedLoadMegaphoneNotificationBootData's two REQUEST_SPACES chunks, in
// Slack's order: a change in Slack's list fails here first.
func TestMegaphoneBootChunks(t *testing.T) {
	first := []string{"client_footer", "client_header_right", "message_pane", "sidebar_footer", "launch_modal",
		"in_product_coachmark", "in_product_glow", "education_user_group_sections_coachmark", "quick_reactions",
		"message_actions_labels", "welcome_space", "message_input", "activity_tab_sidebar", "activity_inbox_header",
		"tab_rail_home_tab", "admin_menu_bottom_banner", "sidebar_popout"}
	second := []string{"workspace_menu_primary_plan_info", "workspace_menu_secondary_plan_info", "workspace_menu_upsell",
		"emoji_drawer", "invite_modal_learn_more_about_guests_cta", "create_channel_make_shared_cta", "guests_modal_dropdown",
		"main_menu", "profile_menu", "sidebar_menu_header", "workflow_builder_header", "app_header", "all_workflows_header",
		"workflows_more_actions_menu", "more_peek_menu", "search_modal_footer", "huddle_invite_modal", "huddle_toolbar",
		"automations_tab_coachmarks", "search_modal_header", "workspace_add_menu", "workspace_team_switcher_menu",
		"canvas_create_menu", "generic_new_badge", "sidebar_menu_seasonal_link", "sidebar_menu_onboarding_tasks_link",
		"slackbot_ai_dm_thread_header", "slackbot_ai_dm", "tools_tab_rail", "tools_tab_sidebar_header",
		"tools_tab_coachmarks", "focus_mode_button", "message_composer"}
	if !slices.Equal(megaphoneBootSpaces, first) {
		t.Errorf("the first chunk is %v (%d), Slack's is %v (%d)", megaphoneBootSpaces, len(megaphoneBootSpaces), first, len(first))
	}
	if !slices.Equal(megaphoneBootSpaces2, second) {
		t.Errorf("the second chunk is %v (%d), Slack's is %v (%d)", megaphoneBootSpaces2, len(megaphoneBootSpaces2), second, len(second))
	}
	for _, sp := range append(slices.Clone(first), second...) {
		if !megaphoneSpaces[sp] {
			t.Errorf("boot space %s isn't one of xu's", sp)
		}
	}
	for sp := range megaphoneAlwaysGlobal {
		if !megaphoneSpaces[sp] {
			t.Errorf("always-global space %s isn't one of xu's", sp)
		}
	}
}

// loadMegaphoneNotificationData files only the answered spaces xu lists:
// later_page_today (gone from xu) answered with a notification is
// dropped, pricing_page_info_banner's is kept. Neither is a boot space, so
// the boot load rtApp sets off can't file over them.
func TestMegaphoneDropsUnknownSpaces(t *testing.T) {
	a, fs, _ := rtApp(t)
	fs.SetMegaphoneSpace("later_page_today", "synthetic_later_notice")
	fs.SetMegaphoneSpace("pricing_page_info_banner", "synthetic_pricing_notice")
	ev := map[string]any{"type": megaphoneRequestSpaces, "spaces": []string{"later_page_today", "pricing_page_info_banner"}, "ia_version": 4}
	out, err := a.loadMegaphone(context.Background(), ev, "", megaphoneReasonUser, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.MegaphoneNotification("pricing_page_info_banner", ""); got != "synthetic_pricing_notice" {
		t.Errorf("pricing_page_info_banner holds %q, want its notification", got)
	}
	if got := a.MegaphoneNotification("later_page_today", ""); got != "" {
		t.Errorf("later_page_today, a space Slack's client doesn't know, holds %q", got)
	}
	if _, ok := out["later_page_today"]; ok {
		t.Errorf("the load answered later_page_today as filed: %v", out)
	}
}

// A load made for a channel files its answers under the channel, except a
// notification answered with no name: 17Qm's two ze names, which the
// desktop build's ze (oCWb) lacks, compare with undefined, so that one is
// filed globally. pricing_page_info_banner, global first, holds the
// nameless answer's (none) afterwards; a named answer stays the channel's.
func TestMegaphoneNamelessFiledGlobally(t *testing.T) {
	a, fs, _ := rtApp(t)
	ctx := context.Background()
	const sp = "pricing_page_info_banner"
	ev := map[string]any{"type": megaphoneRequestSpaces, "spaces": []string{sp}, "ia_version": 4}
	fs.SetMegaphoneSpace(sp, "synthetic_pricing_notice")
	if _, err := a.loadMegaphone(ctx, ev, "", megaphoneReasonUser, nil); err != nil {
		t.Fatal(err)
	}
	if got := a.MegaphoneNotification(sp, ""); got != "synthetic_pricing_notice" {
		t.Fatalf("control: the global load filed %q", got)
	}
	ch := fs.ChannelIDs()[1]
	if _, err := a.loadMegaphone(ctx, ev, ch, megaphoneReasonUser, nil); err != nil {
		t.Fatal(err)
	}
	if got := a.MegaphoneNotification(sp, ch); got != "synthetic_pricing_notice" {
		t.Errorf("control: a named answer to a channel load is the channel's: %q", got)
	}
	fs.SetMegaphoneNameless(sp)
	if _, err := a.loadMegaphone(ctx, ev, ch, megaphoneReasonUser, nil); err != nil {
		t.Fatal(err)
	}
	if got := a.MegaphoneNotification(sp, ""); got != "" {
		t.Errorf("a nameless answer to a channel load left the global space holding %q: Slack files it globally", got)
	}
	if got := a.MegaphoneNotification(sp, ch); got != "synthetic_pricing_notice" {
		t.Errorf("a nameless answer to a channel load changed the channel's space to %q", got)
	}
}
