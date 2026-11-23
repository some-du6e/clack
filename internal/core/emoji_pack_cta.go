package core

// The emoji picker's emoji pack call to action (boot-shared +y5+
// EmojiPackCta, A1Xe, UY+B, bJm9, nkcv; build a09b625d90aa): the picker
// shows it when showAnyEmojiPacksCTA (its mapStateToProps:
// !forceHideEmojiPackCTA && !standardEmojiOnly && (o || B)):
//
//   - o, before the newest pack is installed: the packs are fetched, the
//     Halloween pack isn't out of season, the viewer may add custom emoji
//     (Llxp D), the newest pack isn't installed, the packs became
//     available less than 14 days ago (UY+B fl:
//     emoji_packs_most_recent_available_time) and
//     emoji_packs_clicked_picker_cta isn't set;
//   - B, once it is: emoji_packs_clicked_picker_post_install_cta isn't
//     set, the newest pack is installed, not out of season, within the 14
//     days, and the picker lists a group under its name.
//
// The Halloween pack ("Halloween", ArAs AK) is in season in October
// (isHalloweenThemeSeason: getMonth() === 9) with the
// slack_seasonal_delight experiment on (nkcv k); out of season neither
// call to action shows for it.

import (
	"context"
	"encoding/json"
	"slices"
	"time"
)

// HalloweenEmojiPack is ArAs AK, the seasonal pack's name.
const HalloweenEmojiPack = "Halloween"

// PackCTA is which of the picker's emoji pack calls to action shows.
type PackCTA int

const (
	PackCTANone        PackCTA = iota
	PackCTAInstall             // A1Xe o: "{pack} emoji for your team"
	PackCTAPostInstall         // A1Xe B: "{pack} emoji added!"
)

// SetEmojiPackClock sets the clock the Halloween season reads (tests).
func (a *App) SetEmojiPackClock(now func() time.Time) {
	ps := a.packs()
	ps.mu.Lock()
	ps.now = now
	ps.mu.Unlock()
}

func (a *App) packNow() time.Time {
	ps := a.packs()
	ps.mu.Lock()
	f := ps.now
	ps.mu.Unlock()
	if f != nil {
		return f()
	}
	return time.Now()
}

// EmojiPackCanAdd is Llxp D as the last pack read found it.
func (a *App) EmojiPackCanAdd() bool {
	ps := a.packs()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.canAdd
}

// HalloweenSeason is EmojiPackCta's `_`: isHalloweenThemeSeason (October,
// in the viewer's local time) and slack_seasonal_delight on.
func (a *App) HalloweenSeason() bool {
	return a.packNow().Month() == time.October && a.experiment("slack_seasonal_delight") == "on"
}

// halloweenOutOfSeason is A1Xe's isHalloweenPackOutOfSeason.
func (a *App) halloweenOutOfSeason(newest EmojiPack, ok bool) bool {
	return ok && newest.Name == HalloweenEmojiPack && !a.HalloweenSeason()
}

// EmojiPickerPackCTA is the picker's showAnyEmojiPacksCTA for the groups
// it lists (the installed packs' names among them): which call to action
// shows, and the newest pack it's about (Name "" when there's none, as
// Slack's).
func (a *App) EmojiPickerPackCTA(groups []string) (EmojiPack, PackCTA) {
	ps := a.packs()
	ps.mu.Lock()
	list, fetched, canAdd := append([]EmojiPack(nil), ps.list...), ps.fetched, ps.canAdd
	ps.mu.Unlock()
	newest, ok := newestPack(list)
	oos := a.halloweenOutOfSeason(newest, ok)
	recent := a.EmojiPacksRecent()
	// o
	if fetched && !oos && canAdd && !(ok && newest.Installed) && recent && !a.PrefBool(PrefEmojiPacksPickerCTA, false) {
		return newest, PackCTAInstall
	}
	// B
	if !a.PrefBool(PrefEmojiPacksPostInstallCTA, false) && ok && newest.Installed && !oos && recent &&
		slices.Contains(groups, newest.Name) {
		return newest, PackCTAPostInstall
	}
	return EmojiPack{}, PackCTANone
}

// ClickedEmojiPickerCTA is EmojiPackCta's onCtaClicked (a click, the
// dismiss button): setUserPrefByApi on the call to action's pref,
// emoji_packs_clicked_picker_cta or, after the install,
// emoji_packs_clicked_picker_post_install_cta; the local pref first, then
// users.prefs.set.
func (a *App) ClickedEmojiPickerCTA(post bool) {
	name := PrefEmojiPacksPickerCTA
	if post {
		name = PrefEmojiPacksPostInstallCTA
	}
	a.mergePrefs(map[string]json.RawMessage{name: json.RawMessage("true")})
	a.emit(Event{Type: "emoji_packs"})
	go func() {
		ctx, cancel := context.WithTimeout(a.bgCtx(), 30*time.Second)
		defer cancel()
		_ = a.SetPref(ctx, name, true)
	}()
}
