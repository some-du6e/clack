package core

// Megaphone, Slack's in-product notification service, the way the web
// client keeps it (client-boot: the megaphoneNotification store ZNtt, its
// selectors NWiV, loadMegaphoneNotificationData and the boot load i2+v,
// channelOpenedEvent fRek, the RTM handlers in client-boot-render; CDN
// build bv1-13):
//
//   - Two stores: the global spaces (megaphoneSpaces) and per channel
//     channelSpaces. A load made for a channel (REQUEST_SPACES or
//     CHANNEL_OPEN with channelId) files its answers under that channel,
//     except the spaces in megaphoneAlwaysGlobal; a load without one (the
//     boot load, user_refresh) files them globally. A space the server
//     answered null holds "".
//   - The boot load (chunkedLoadMegaphoneNotificationBootData, memoized
//     for an hour): REQUEST_SPACES for its first chunk of spaces,
//     message_input among them, with event.channel the open conversation
//     when it looks like a channel id, iaVersion 4, isAppStoreBuild and
//     isJoinerLaunchpadEnabled (false here: no Mac App Store build, no
//     joiner launchpad); reason nav-boot.
//   - channelOpenedEvent: CHANNEL_OPEN for the Slackbot DM and externally
//     shared channels only (shouldDispatchChannelOpenEvent), memoized.
//   - RTM: megaphone_delayed_notification_ready sets a global space;
//     clear_megaphone_notification drops a notification from the spaces
//     showing it; megaphone_notifications_user_refresh reloads the spaces
//     it names globally (or runs the boot load);
//     megaphone_notifications_team_refresh clears every channel's spaces,
//     runs the boot load after a random delay (up to 20 s) and sends
//     CHANNEL_OPEN for the open channel.
//
// Each change emits Event{Type: "megaphone"}, Channel the channel whose
// spaces changed ("" for the global ones).

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/url"
	"regexp"
	"sync"
	"time"
)

// Megaphone spaces and notifications Clack reads (hro4 xu, ze).
const (
	SpaceMessageInputInvitedGlow     = "message_input_invited_glow"
	SpaceMessageInputJoinSummaryGlow = "message_input_channel_join_summary_glow"

	megaphoneChannelOpen = "channel_open"
	megaphoneReasonBoot  = "nav-boot"
	megaphoneReasonUser  = "user_refresh"
	megaphoneBootTTL     = time.Hour
	megaphoneMemoTTL     = time.Hour
)

// MegaphoneMessageInputNotifications is the message list's megaphone
// container for message_input (GyIg G5): the notifications whose banner
// the message list renders above the input. ChannelJoinBanner is the one
// for education_channel_join_banner.
var MegaphoneMessageInputNotifications = []string{
	NotificationChannelJoinBanner,
	"message_input_channel_archive_banner",
	"slack_ai_push_summaries_prompt",
	"pro_trial_slack_ai_offer_in_channel_summary_banner",
	"plus_trial_slack_ai_offer_in_channel_recap_banner",
	"plus_request_to_purchase_channel_banner",
}

// megaphoneBootSpaces2 is the boot load's second chunk (i2+v's l[1]),
// loaded beside the first: the menus' and pages' spaces, among them
// generic_new_badge, whose badges list turns on Slack's "New" tags
// (temp_mute_new_badge on the bell's More options).
var megaphoneBootSpaces2 = []string{
	"workspace_menu_primary_plan_info", "workspace_menu_secondary_plan_info", "workspace_menu_upsell", "emoji_drawer",
	"invite_modal_learn_more_about_guests_cta", "create_channel_make_shared_cta", "guests_modal_dropdown", "main_menu",
	"profile_menu", "sidebar_menu_header", "later_page_today", "workflow_builder_header", "app_header",
	"all_workflows_header", "workflows_more_actions_menu", "more_peek_menu", "search_modal_footer",
	"huddle_invite_modal", "huddle_toolbar", "automations_tab_coachmarks", "search_modal_header",
	"workspace_add_menu", "workspace_team_switcher_menu", "canvas_create_menu", SpaceGenericNewBadge,
	"sidebar_menu_seasonal_link", "sidebar_menu_onboarding_tasks_link", "slackbot_ai_dm_thread_header",
	"slackbot_ai_dm", "tools_tab_rail", "tools_tab_sidebar_header", "tools_tab_coachmarks", "focus_mode_button",
	"message_composer",
}

// SpaceGenericNewBadge is hro4 xu.GENERIC_NEW_BADGE.
const SpaceGenericNewBadge = "generic_new_badge"

// megaphoneBootSpaces is the boot load's first chunk.
var megaphoneBootSpaces = []string{
	"client_footer", "client_header_right", "message_pane", "sidebar_footer", "launch_modal",
	"in_product_coachmark", "in_product_glow", "education_user_group_sections_coachmark",
	"quick_reactions", "message_actions_labels", "welcome_space", SpaceMessageInput,
	"activity_tab_sidebar", "activity_inbox_header", "tab_rail_home_tab", "admin_menu_bottom_banner",
	"sidebar_popout",
}

// megaphoneAlwaysGlobal are the spaces a channel load still files
// globally (i2+v's M).
var megaphoneAlwaysGlobal = map[string]bool{
	"record_channel_details_tab_header":               true,
	"record_channel_related_conversations_tab_header": true,
	"record_channel_related_record_list_tab_header":   true,
	"record_channel_summary_tab_header":               true,
	SpaceMessageInputInvitedGlow:                      true,
	SpaceMessageInputJoinSummaryGlow:                  true,
	"discovery_dot":                                   true,
}

// LooksLikeChannelID is jh4t's looksLikeChannelId (the boot load's
// event.channel, maybeHideChannelJoinBanner's channel view).
var LooksLikeChannelID = regexp.MustCompile(`^(C|G|D|Z)[A-Z0-9]{8,}$`).MatchString

type megaphoneState struct {
	mu       sync.Mutex
	global   map[string]string
	channels map[string]map[string]string
	// badges is the global generic_new_badge space's notificationData.badges
	// (the "New" tags its notification turns on; NWiV ap reads them)
	badges  []string
	bootAt  time.Time            // the last boot load (memoized for an hour)
	memo    map[string]time.Time // memoized loads (CHANNEL_OPEN) by request
	delayed bool                 // a random-delay boot load is pending
}

// MegaphoneNotification is selectMegaphoneNotification's name: space's
// notification in ch's spaces, or the global one for ch "". "" when the
// space holds none.
func (a *App) MegaphoneNotification(space, ch string) string {
	m := &a.mega
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch != "" {
		return m.channels[ch][space]
	}
	return m.global[space]
}

// megaphoneSet is setMegaphoneNotification (G0).
func (a *App) megaphoneSet(space, ch, name string) {
	m := &a.mega
	m.mu.Lock()
	if ch == "" {
		if m.global == nil {
			m.global = map[string]string{}
		}
		m.global[space] = name
	} else {
		if m.channels == nil {
			m.channels = map[string]map[string]string{}
		}
		if m.channels[ch] == nil {
			m.channels[ch] = map[string]string{}
		}
		m.channels[ch][space] = name
	}
	m.mu.Unlock()
}

// loadMegaphone is loadMegaphoneNotificationData: megaphone.notifications.list
// {event, channel_id, reason} and each answered space filed as Slack
// files it. extra are more form fields (is_team_on_trial). It returns
// space -> notification name as answered.
func (a *App) loadMegaphone(ctx context.Context, event map[string]any, ch, reason string, extra url.Values) (map[string]string, error) {
	ev, _ := json.Marshal(event)
	p := url.Values{"event": {string(ev)}, "reason": {reason}}
	if ch != "" {
		p.Set("channel_id", ch)
	}
	for k, v := range extra {
		p[k] = v
	}
	var r struct {
		Notifications map[string]*struct {
			Name string          `json:"name"`
			Data json.RawMessage `json:"data"`
		} `json:"notifications"`
	}
	if err := a.C.Call(ctx, "megaphone.notifications.list", p, &r); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(r.Notifications))
	for space, n := range r.Notifications {
		name := ""
		if n != nil {
			name = n.Name
		}
		out[space] = name
		at := ch
		if megaphoneAlwaysGlobal[space] {
			at = ""
		}
		a.megaphoneSet(space, at, name)
		if space == SpaceGenericNewBadge && at == "" {
			var d struct {
				Badges []string `json:"badges"`
			}
			if n != nil {
				_ = json.Unmarshal(n.Data, &d)
			}
			a.mega.mu.Lock()
			a.mega.badges = d.Badges
			a.mega.mu.Unlock()
		}
	}
	a.emit(Event{Type: "megaphone", Channel: ch})
	return out, nil
}

// MegaphoneBoot is maybeFetchMegaphoneOnBootEvent's boot load, at most
// once an hour; open is the conversation on screen ("" none).
func (a *App) MegaphoneBoot(ctx context.Context, open string) error {
	m := &a.mega
	m.mu.Lock()
	if !m.bootAt.IsZero() && time.Since(m.bootAt) < megaphoneBootTTL {
		m.mu.Unlock()
		return nil
	}
	m.bootAt = time.Now()
	m.mu.Unlock()
	return a.megaphoneBootLoad(ctx, open, megaphoneReasonBoot)
}

func (a *App) megaphoneBootLoad(ctx context.Context, open, reason string) error {
	// chunkedLoadMegaphoneNotificationBootData: both chunks at once
	var err2 error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err2 = a.loadMegaphone(ctx, megaphoneBootEvent(megaphoneBootSpaces2, open), "", reason, nil)
	}()
	_, err := a.loadMegaphone(ctx, megaphoneBootEvent(megaphoneBootSpaces, open), "", reason, nil)
	<-done
	if err == nil {
		err = err2
	}
	return err
}

// megaphoneBootEvent is one boot chunk's REQUEST_SPACES event.
func megaphoneBootEvent(spaces []string, open string) map[string]any {
	ev := map[string]any{"type": megaphoneRequestSpaces, "spaces": spaces, "ia_version": 4,
		"is_app_store_build": false, "is_joiner_launchpad_enabled": false}
	if LooksLikeChannelID(open) {
		ev["channel"] = open
	}
	return ev
}

// MegaphoneNewBadge is isNewBadgeShowing (NWiV ap): the global
// generic_new_badge space's badges list has badge.
func (a *App) MegaphoneNewBadge(badge string) bool {
	m := &a.mega
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.badges {
		if b == badge {
			return true
		}
	}
	return false
}

// ClearNewBadge is clearNewBadge (ZNtt h1): badge leaves the list here
// only (Slack sends nothing; the server stops answering it once the
// notification's impressions are in).
func (a *App) ClearNewBadge(badge string) {
	m := &a.mega
	m.mu.Lock()
	var keep []string
	found := false
	for _, b := range m.badges {
		if b == badge {
			found = true
			continue
		}
		keep = append(keep, b)
	}
	m.badges = keep
	m.mu.Unlock()
	if found {
		a.emit(Event{Type: "megaphone"})
	}
}

// MegaphoneChannelOpened is channelOpenedEvent: CHANNEL_OPEN for ch when
// it is the Slackbot DM or an externally shared channel, memoized for an
// hour (O = memoize(loadMegaphoneNotificationData, 36e5)).
func (a *App) MegaphoneChannelOpened(ctx context.Context, ch string) error {
	cv, ok := a.Conv(ch)
	if !ok || !(cv.Kind == "im" && cv.User == "USLACKBOT" || cv.IsShared) {
		return nil
	}
	return a.megaphoneChannelOpen(ctx, ch, true)
}

// megaphoneChannelOpen loads CHANNEL_OPEN for ch: channelOpenedEvent's
// memoized load with isTeamOnTrial (memo), or team_refresh's plain one.
func (a *App) megaphoneChannelOpen(ctx context.Context, ch string, memo bool) error {
	var extra url.Values
	if memo {
		extra = url.Values{"is_team_on_trial": {"false"}}
		m := &a.mega
		m.mu.Lock()
		if at, ok := m.memo[ch]; ok && time.Since(at) < megaphoneMemoTTL {
			m.mu.Unlock()
			return nil
		}
		if m.memo == nil {
			m.memo = map[string]time.Time{}
		}
		m.memo[ch] = time.Now()
		m.mu.Unlock()
	}
	_, err := a.loadMegaphone(ctx, map[string]any{"type": megaphoneChannelOpen, "channel_id": ch}, ch, megaphoneReasonLoad, extra)
	return err
}

// onMegaphoneEvent handles the megaphone RTM events (client-boot-render's
// handlers). The loads run off the event pump on the session's context.
func (a *App) onMegaphoneEvent(typ string, raw json.RawMessage) {
	load := func(f func(ctx context.Context)) {
		go func() {
			ctx, cancel := context.WithTimeout(a.bgCtx(), 20*time.Second)
			defer cancel()
			f(ctx)
		}()
	}
	switch typ {
	case "megaphone_delayed_notification_ready":
		var e struct {
			Space string `json:"space"`
			Name  string `json:"notification_name"`
		}
		if json.Unmarshal(raw, &e) != nil || e.Space == "" {
			return
		}
		a.megaphoneSet(e.Space, "", e.Name)
		a.emit(Event{Type: "megaphone"})
	case "clear_megaphone_notification":
		var e struct {
			Notification string `json:"notification"`
			Channel      string `json:"channel"`
		}
		if json.Unmarshal(raw, &e) != nil {
			return
		}
		m := &a.mega
		m.mu.Lock()
		spaces := m.global
		if e.Channel != "" {
			spaces = m.channels[e.Channel]
		}
		for sp, n := range spaces {
			if n == e.Notification {
				delete(spaces, sp)
				if sp == SpaceGenericNewBadge && e.Channel == "" {
					m.badges = nil
				}
			}
		}
		m.mu.Unlock()
		a.emit(Event{Type: "megaphone", Channel: e.Channel})
	case "megaphone_notifications_user_refresh":
		var e struct {
			Spaces []string `json:"spaces"`
		}
		_ = json.Unmarshal(raw, &e)
		load(func(ctx context.Context) {
			if len(e.Spaces) > 0 {
				_, _ = a.loadMegaphone(ctx, map[string]any{"type": megaphoneRequestSpaces, "spaces": e.Spaces,
					"is_app_store_build": false, "ia_version": 4}, "", megaphoneReasonUser, nil)
				return
			}
			_ = a.megaphoneBootLoad(ctx, a.selectedID(), megaphoneReasonUser)
		})
	case "megaphone_notifications_team_refresh":
		m := &a.mega
		m.mu.Lock()
		m.channels = nil
		delay := !m.delayed
		m.delayed = true
		m.mu.Unlock()
		a.emit(Event{Type: "megaphone"})
		if delay {
			wait := time.Duration(rand.Int64N(int64(20 * time.Second)))
			go func() {
				select {
				case <-a.bgCtx().Done():
					return
				case <-time.After(wait):
				}
				m.mu.Lock()
				m.delayed = false
				m.mu.Unlock()
				load(func(ctx context.Context) { _ = a.megaphoneBootLoad(ctx, a.selectedID(), megaphoneReasonBoot) })
			}()
		}
		if open := a.selectedID(); LooksLikeChannelID(open) {
			load(func(ctx context.Context) { _ = a.megaphoneChannelOpen(ctx, open, false) })
		}
	}
}
