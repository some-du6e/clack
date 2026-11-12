package core

// Channel details' header "Start a Call ▾" and About's "Send emails to
// this channel" (docs/GAPS-2026-11-11.md gap 14, work package W13), read
// from Slack's web client on the public CDN (build d3de3a487b1c,
// 2026-11-12). The UI is views/details_call.go and popups/channel_email.go.
//
// Calls. The header button is CallButton (the use-slackbot-skills-tab
// chunk's z2Iw, useIaDetailsStyle) around WithCallMenu (boot-shared CXZJ)
// and CallMenu (GPML). The call apps are the team pref calls_apps
// ({video: [...], audio: [...]}, each {id, name, image}; KkHb):
//
//   - getFlattenTeamPrefCallsApps (KkHb wZ): video then audio, without
//     Slack's retired calls app A00. numCallApps is its length: 0 makes
//     the button "Huddle" with the headphones glyph, more than 1 adds the
//     chevron.
//   - getCallsAppsFromTeamPref (KkHb q8): the same lists, audio dropped
//     for a channel (only DMs dial phones).
//   - getCallAppList (KkHb OJ): the video apps (unless hidden) as type
//     video, then the audio ones as type audio when the member has a phone
//     number (wKba: set and not "-").
//   - getAppsById (KkHb vU): the Zoom app ids draw Slack's zoom icon.
//   - isCallButtonVisibleForChannel (5R3h Xk) and
//     isCallButtonDisabledForChannel (5R3h Rv), the tooltip
//     (getTipForStartCall j7, getTipSubtitleForStartCall tw) and the
//     confirmation strings (uI) are in the view; the reads they need are
//     here (CallsAllowed, CanCallMember).
//   - startCall (AslI t3 → startPlatformCall): calls.request {channel,
//     app, type, phone_number (audio only)} answers {url,
//     desktop_protocol_url}; Slack opens the protocol URL when the
//     desktop app has a handler for it, else the URL (encodeURI'd except
//     for a few apps). Clack can't ask the OS for a handler, so it opens
//     the URL. A failure is Slackbot's temporary message in the
//     conversation (handleCallsRequestError: the error's response, else
//     "Darn, that didn’t work…").
//
// Emails. SendEmailsToChannelButton (about-channel-modal Qjs2) opens the
// channel email modal (boot-shared fRXX). Its reads and writes (QOnw,
// sW/H, GuCY, uFgA, nSb3):
//
//   - callGetChannelEmailWithChannelId (QOnw e2): rejected on a free team;
//     conversations.emailaddresses.info {conversation_id}, then, when it
//     has no address and createIfNotExists, conversations.emailaddresses.
//     create {conversation_id}.
//   - getChannelEmailAddress (sW/H): the answer normalised (GuCY Nx:
//     conversation_email_address — a new one, createdNew — or the first of
//     conversation_email_addresses) and kept in channelEmails when it has
//     an address; a failure answers what's kept, else {isError}.
//   - updateChannelEmailAddress (fRXX q, QOnw tK):
//     conversations.emailaddresses.update {conversation_id, avatar_id,
//     name, emoji, address, use_default_icon}; deleteChannelEmailAddress
//     (fRXX Q, QOnw lx): conversations.emailaddresses.delete
//     {conversation_id, address}. Both need a kept address.
//   - the icon upload: avatars.upload {image} (hdGD), then avatars.crop
//     {avatar, cropbox "x,y,w"} (jFSr).
//   - showChannelEmailAddress (uFgA ut), showChannelEmailAddressUpgradePrompt
//     (uFgA CI) and isChannelEmailAddressEnabledByOrg (uFgA iL).

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slack"
)

// Call app ids with a meaning of their own.
const (
	// CallAppLegacy is Slack's retired calls app: never listed.
	CallAppLegacy = "A00"
	// CallAppHuddle is getAppsById's "Start a huddle" entry: CallMenu
	// draws the huddle rows in its place.
	CallAppHuddle = "A01"
)

// Call types (KkHb Hu).
const (
	CallVideo = "video"
	CallAudio = "audio"
)

// zoomAppIDs are the apps getAppsById draws with Slack's zoom icon.
var zoomAppIDs = map[string]bool{"A5GE9BMQC": true, "A649AA069": true, "A5W4BRDNX": true, "ARQS1U88Y": true, "ARUFE7YMU": true}

// rawURLCallApps are the apps whose calls.request URL Slack opens as it
// came (startPlatformCall's w); the rest go through encodeURI.
var rawURLCallApps = map[string]bool{"AD6B97QES": true, "A9SD542KA": true, "ABE6T3L7Q": true, "ADHD298UB": true,
	"A5P5FDK33": true, "AN2CSGQUR": true, "ANAAG9CKA": true, "AN1CDQZKL": true, "AN80VL39P": true}

// zoomIcon is getAppsById's image for the Zoom apps (a path on the web
// client's origin).
const zoomIcon = "https://app.slack.com/marketing/img/homepage/icons/zoom-icon.png"

// DefaultCallAppIcon is CallMenu's icon for an app without an image
// (i+lO's default, client-boot OYli).
const DefaultCallAppIcon = "https://a.slack-edge.com/bv1-13/default_new_app_icon-936d613.png"

// CallApp is one calls_apps entry; Type is set by CallAppList.
type CallApp struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image,omitempty"`
	Type  string `json:"-"`
}

// CallsApps are the team's video and audio call apps.
type CallsApps struct {
	Video []CallApp `json:"video"`
	Audio []CallApp `json:"audio"`
}

// teamCallsApps is filterCallsAppsFromTeamPref(state, true): the team
// pref without A00 among the video apps. (Slack also drops apps the app
// store marks unavailable, cwbZ z, which Clack doesn't load.)
func (a *App) teamCallsApps() CallsApps {
	var c CallsApps
	if raw, ok := a.TeamPref("calls_apps"); ok {
		_ = json.Unmarshal(raw, &c)
	}
	video := c.Video[:0:0]
	for _, v := range c.Video {
		if v.ID != CallAppLegacy {
			video = append(video, v)
		}
	}
	c.Video = video
	return c
}

// CallsAppsFor is getCallsAppsFromTeamPref(state, isChannel(ch)): a
// public or private channel gets no audio apps.
func (a *App) CallsAppsFor(ch string) CallsApps {
	c := a.teamCallsApps()
	if k := a.kindOf(ch); k == "channel" || k == "private" {
		c.Audio = nil
	}
	return c
}

// NumCallApps is getFlattenTeamPrefCallsApps(state).length.
func (a *App) NumCallApps() int {
	c := a.teamCallsApps()
	return len(c.Video) + len(c.Audio)
}

// ThirdPartyCallEnabled is isThirdPartyCallEnabled (5R3h): an app other
// than A00 among the flattened apps.
func (a *App) ThirdPartyCallEnabled() bool { return a.NumCallApps() > 0 }

// CallsAllowed is isCallsAllowedByAdmin (5R3h): the allow_calls team pref.
func (a *App) CallsAllowed() bool {
	raw, _ := a.TeamPref("allow_calls")
	var b bool
	return json.Unmarshal(raw, &b) == nil && b
}

// ValidPhone is isValidFieldValue (wKba): set and not "-".
func ValidPhone(s string) bool { return s != "" && s != "-" }

// CallAppList is getCallAppList: the video apps as type video (unless
// hideVideo), then the audio apps as type audio when phone is valid.
func CallAppList(c CallsApps, phone string, hideVideo bool) []CallApp {
	var out []CallApp
	if !hideVideo {
		for _, v := range c.Video {
			v.Type = CallVideo
			out = append(out, v)
		}
	}
	if ValidPhone(phone) {
		for _, v := range c.Audio {
			v.Type = CallAudio
			out = append(out, v)
		}
	}
	return out
}

// CallAppImage is the image getAppsById gives app ("" when it has none:
// the menu then draws DefaultCallAppIcon).
func CallAppImage(app CallApp) string {
	if zoomAppIDs[app.ID] {
		return zoomIcon
	}
	return app.Image
}

// MemberPhone is getMemberPhone: the person's profile phone.
func (a *App) MemberPhone(user string) string {
	if u, ok := a.S.User(user); ok {
		return u.Profile.Phone
	}
	return ""
}

// CanCallMember is canCallMember (5R3h hL) as far as Clack knows the
// person: calls or huddles allowed (huddles from the UI's
// doesChannelSupportHuddles), not yourself, and an active member.
func (a *App) CanCallMember(user string, huddles bool) bool {
	u, ok := a.S.User(user)
	if !ok {
		return false
	}
	if !a.CallsAllowed() && !huddles {
		return false
	}
	return user != a.S.SelfID() && !u.Deleted
}

// StartCallResult is calls.request's answer.
type StartCallResult struct {
	URL                string `json:"url"`
	DesktopProtocolURL string `json:"desktop_protocol_url"`
}

// OpenURL is what startPlatformCall opens without a protocol handler:
// the URL, through encodeURI unless the app is one Slack leaves alone.
func (r StartCallResult) OpenURL(app string) string {
	if rawURLCallApps[app] {
		return r.URL
	}
	return encodeURI(r.URL)
}

// encodeURI is JavaScript's encodeURI: percent-encodes everything but
// the URI's reserved and unreserved characters and '#'.
func encodeURI(s string) string {
	const keep = "-_.!~*'();/?:@&=+$,#"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte(keep, c) >= 0:
			b.WriteByte(c)
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b.WriteByte(c) // an escape already in the URL (encodeURI would double it; Slack's URLs have none)
		default:
			const hex = "0123456789ABCDEF"
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// StartCall is startPlatformCall: calls.request for app in ch. A00 (and
// no app) does nothing. On failure it posts Slackbot's temporary message
// in ch and returns the error.
func (a *App) StartCall(ctx context.Context, ch, app, typ, phone string) (StartCallResult, error) {
	if app == "" || app == CallAppLegacy {
		return StartCallResult{}, errors.New("no call app")
	}
	p := url.Values{"channel": {ch}, "app": {app}, "type": {typ}}
	if typ == CallAudio && phone != "" {
		p.Set("phone_number", phone)
	}
	var r struct {
		StartCallResult
		Response string `json:"response"`
	}
	err := a.C.Call(ctx, "calls.request", p, &r)
	if err != nil {
		text := r.Response
		var se *slack.Error
		if errors.As(err, &se) && len(se.Body) > 0 {
			var b struct {
				Response string `json:"response"`
			}
			if json.Unmarshal(se.Body, &b) == nil && b.Response != "" {
				text = b.Response // the error's response (t.response)
			}
		}
		if text == "" {
			text = nsStartCall.T("Darn, that didn’t work. Try your call again or <{url}|contact us> if it’s still not working.",
				"url", "https://my.slack.com/help/requests/new")
		}
		a.LocalEphemeral(ch, "", text)
		return StartCallResult{}, err
	}
	return r.StartCallResult, nil
}

// nsStartCall is startPlatformCall's I18n("start_call").
var nsStartCall = i18n.NS("start_call")

// ---- channel email addresses ----

// ChannelEmail is a channelEmails entry (GuCY Nx).
type ChannelEmail struct {
	ChannelID string
	Address   string
	Name      string
	Icons     ChannelEmailIcons
	// CreatedNew: conversations.emailaddresses.create just made it.
	CreatedNew bool
	// Err is getChannelEmailAddress's {isError}.
	Err bool
}

// ChannelEmailIcons are an address's icons.
type ChannelEmailIcons struct {
	Image36       string `json:"image_36,omitempty"`
	Image48       string `json:"image_48,omitempty"`
	Image72       string `json:"image_72,omitempty"`
	ImageOriginal string `json:"image_original,omitempty"`
	Emoji         string `json:"emoji,omitempty"`
}

// Image is GuCY EA: the first of image_36, 48, 72 and original.
func (i ChannelEmailIcons) Image() string {
	for _, s := range []string{i.Image36, i.Image48, i.Image72, i.ImageOriginal} {
		if s != "" {
			return s
		}
	}
	return ""
}

// DefaultChannelEmailIcon is the customize pane's preview for the
// default icon (fRXX V, boot-render DZYh).
const DefaultChannelEmailIcon = "https://a.slack-edge.com/bv1-13/bot_36-72be7ac.png"

// IsDefaultEmailIcon is the customize pane's test for the plugin's own
// icon (url includes "plugins/channel_email_address").
func IsDefaultEmailIcon(u string) bool { return strings.Contains(u, "plugins/channel_email_address") }

type emailAddrJSON struct {
	Address string            `json:"address"`
	Name    string            `json:"name"`
	Icons   ChannelEmailIcons `json:"icons"`
}

type emailAnswer struct {
	One  *emailAddrJSON  `json:"conversation_email_address"`
	Many []emailAddrJSON `json:"conversation_email_addresses"`
}

// normalise is GuCY Nx.
func (r emailAnswer) normalise(ch string) ChannelEmail {
	e := ChannelEmail{ChannelID: ch}
	switch {
	case r.One != nil:
		e.Address, e.Name, e.Icons, e.CreatedNew = r.One.Address, r.One.Name, r.One.Icons, true
	case len(r.Many) > 0:
		e.Address, e.Name, e.Icons = r.Many[0].Address, r.Many[0].Name, r.Many[0].Icons
	}
	if e.Address == "" {
		return ChannelEmail{ChannelID: ch}
	}
	return e
}

type emailStore struct {
	mu sync.Mutex
	m  map[string]ChannelEmail // channelEmails
}

var emailIndex sync.Map // *App → *emailStore

func (a *App) emails() *emailStore {
	v, _ := emailIndex.LoadOrStore(a, &emailStore{m: map[string]ChannelEmail{}})
	return v.(*emailStore)
}

// ChannelEmailCached is nSb3 H: channelEmails[ch].
func (a *App) ChannelEmailCached(ch string) (ChannelEmail, bool) {
	s := a.emails()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[ch]
	return e, ok
}

func (a *App) putChannelEmail(e ChannelEmail) {
	s := a.emails()
	s.mu.Lock()
	s.m[e.ChannelID] = e
	s.mu.Unlock()
	a.emit(Event{Type: "channel_email", Channel: e.ChannelID})
}

// ErrFreePlanEmail is callGetChannelEmailWithChannelId's rejection on a
// free team.
var ErrFreePlanEmail = errors.New("conversations.emailaddresses.info failed because team is on free plan")

// GetChannelEmail is getChannelEmailAddress({channelId, createIfNotExists}).
// It never fails: an error answers the kept address, else {Err}.
func (a *App) GetChannelEmail(ctx context.Context, ch string, create bool) ChannelEmail {
	e, err := a.fetchChannelEmail(ctx, ch, create)
	if err == nil {
		if e.Address != "" {
			a.putChannelEmail(e)
		}
		return e
	}
	if kept, ok := a.ChannelEmailCached(ch); ok {
		return kept
	}
	e = ChannelEmail{ChannelID: ch, Err: true}
	a.putChannelEmail(e)
	return e
}

// fetchChannelEmail is callGetChannelEmailWithChannelId.
func (a *App) fetchChannelEmail(ctx context.Context, ch string, create bool) (ChannelEmail, error) {
	if ch == "" {
		return ChannelEmail{}, errors.New("conversations.emailaddresses.info/conversations.emailaddresses.create failed with no channelId as argument")
	}
	if a.OnFreePlan() {
		return ChannelEmail{}, ErrFreePlanEmail
	}
	p := url.Values{"conversation_id": {ch}}
	var r emailAnswer
	if err := a.C.Call(ctx, "conversations.emailaddresses.info", p, &r); err != nil {
		return ChannelEmail{}, err
	}
	if len(r.Many) > 0 || !create {
		return r.normalise(ch), nil
	}
	r = emailAnswer{}
	if err := a.C.Call(ctx, "conversations.emailaddresses.create", p, &r); err != nil {
		return ChannelEmail{}, err
	}
	return r.normalise(ch), nil
}

// EmailUpdate is updateChannelEmailAddress's arguments: one of Icon (an
// avatars id), Emoji (":name:") or UseDefaultIcon.
type EmailUpdate struct {
	Name, Address, Icon, Emoji string
	UseDefaultIcon             bool
}

// UpdateChannelEmail is updateChannelEmailAddress: it needs a kept
// address, and keeps the answer.
func (a *App) UpdateChannelEmail(ctx context.Context, ch string, u EmailUpdate) (ChannelEmail, error) {
	if kept, ok := a.ChannelEmailCached(ch); !ok || kept.Address == "" {
		return ChannelEmail{}, errors.New("an email address does not exist for " + ch)
	}
	p := url.Values{"conversation_id": {ch}, "name": {u.Name}, "address": {u.Address}}
	if u.Icon != "" {
		p.Set("avatar_id", u.Icon)
	}
	if u.Emoji != "" {
		p.Set("emoji", u.Emoji)
	}
	if u.UseDefaultIcon {
		p.Set("use_default_icon", "true")
	}
	var r emailAnswer
	if err := a.C.Call(ctx, "conversations.emailaddresses.update", p, &r); err != nil {
		return ChannelEmail{}, err
	}
	e := r.normalise(ch)
	e.CreatedNew = false
	a.putChannelEmail(e)
	return e, nil
}

// DeleteChannelEmail is deleteChannelEmailAddress: it needs a kept
// address, and forgets it (PzON d3).
func (a *App) DeleteChannelEmail(ctx context.Context, ch, address string) error {
	if kept, ok := a.ChannelEmailCached(ch); !ok || kept.Address == "" {
		return errors.New("an email address does not exist for " + ch)
	}
	if address == "" {
		return errors.New("conversations.emailaddresses.delete failed with no address as argument")
	}
	if err := a.C.Call(ctx, "conversations.emailaddresses.delete", url.Values{"conversation_id": {ch}, "address": {address}}, nil); err != nil {
		return err
	}
	s := a.emails()
	s.mu.Lock()
	delete(s.m, ch)
	s.mu.Unlock()
	a.emit(Event{Type: "channel_email", Channel: ch})
	return nil
}

// EmailIcon is an avatars.upload or avatars.crop answer.
type EmailIcon struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// UploadEmailIcon is EmailIconCropPane's avatars.upload {image}.
func (a *App) UploadEmailIcon(ctx context.Context, name string, data []byte) (EmailIcon, error) {
	var r EmailIcon
	if err := a.C.CallMultipart(ctx, "avatars.upload", url.Values{}, "image", name, data, &r); err != nil {
		return EmailIcon{}, err
	}
	if r.ID == "" || r.URL == "" {
		return EmailIcon{}, errors.New("avatars.upload: no avatar")
	}
	return r, nil
}

// EmailCropMinSize is the email icon cropper's smallest square (fRXX
// EmailIconCropPane: maxSize 460, minSize 36).
const EmailCropMinSize = 36

// EmailCropBox is EmailIconCropPane's cropbox: the square (display px on
// the image shown at dw×dh) in image pixels, "x,y,width", each rounded
// (the scale is at least 1, as Slack's Math.max(1, …)).
func EmailCropBox(x, y, side, dw, dh float64, imgW, imgH int) string {
	sy := math.Max(1, float64(imgH)/dh)
	sx := math.Max(1, float64(imgW)/dw)
	return strconv.Itoa(int(math.Round(x*sx))) + "," + strconv.Itoa(int(math.Round(y*sy))) + "," + strconv.Itoa(int(math.Round(side*sx)))
}

// CropEmailIcon is the crop pane's Save: avatars.crop {avatar, cropbox}.
func (a *App) CropEmailIcon(ctx context.Context, avatar, cropbox string) (EmailIcon, error) {
	var r EmailIcon
	if err := a.C.Call(ctx, "avatars.crop", url.Values{"avatar": {avatar}, "cropbox": {cropbox}}, &r); err != nil {
		return EmailIcon{}, err
	}
	return r, nil
}

// PermCreateChannelEmailAddresses is showChannelEmailAddress's grant
// (QxkB CREATE_CHANNEL_EMAIL_ADDRESSES), read with the details' other
// channel grants (chperms.go).
const PermCreateChannelEmailAddresses = "CREATE_CHANNEL_EMAIL_ADDRESSES"

// emailBase is the conditions showChannelEmailAddress and the upgrade
// prompt share: not archived, not your Slackbot DM, a member, can post,
// and disable_email_ingestion isn't on. (isSelfTeamGovSlack is false off
// GovSlack, which Clack doesn't sign in to.)
func (a *App) emailBase(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok || c.IsArchived || (c.IsIM && c.User == "USLACKBOT") {
		return false
	}
	member := c.IsMember || c.IsIM || c.IsMPIM // a DM is always the viewer's (JUrK bl)
	if !member || !a.PostingFor(c).CanPost() {
		return false
	}
	raw, _ := a.TeamPref("disable_email_ingestion")
	var off bool
	return !(json.Unmarshal(raw, &off) == nil && off) // canUserDoDisableStyleTeamPref: any team not true
}

// paidTeam is isPaidTeam (df7m): team.info has a plan.
func (a *App) paidTeam() bool {
	t := a.teamPlan()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.known && t.plan != ""
}

// ShowChannelEmail is showChannelEmailAddress (uFgA ut): emailBase on a
// paid team with the CREATE_CHANNEL_EMAIL_ADDRESSES grant.
func (a *App) ShowChannelEmail(ch string) bool {
	return a.emailBase(ch) && a.paidTeam() && a.ChannelGrant(ch, PermCreateChannelEmailAddresses)
}

// ChannelEmailUpgradePrompt is showChannelEmailAddressUpgradePrompt
// (uFgA CI): emailBase on a free team. (Slack also skips a frozen plan,
// planInfo.is_plan_frozen, which Clack doesn't read.)
func (a *App) ChannelEmailUpgradePrompt(ch string) bool {
	return a.emailBase(ch) && a.OnFreePlan()
}

// ChannelEmailEnabledByOrg is isChannelEmailAddressEnabledByOrg (uFgA
// iL): off Grid always, on Grid when the org pref
// channel_email_addresses_enabled is set.
func (a *App) ChannelEmailEnabledByOrg() bool {
	t := a.teamPlan()
	t.mu.Lock()
	ent := t.enterpriseID != ""
	t.mu.Unlock()
	if !ent && a.C.Creds.EnterpriseID == "" {
		return true
	}
	return jsTruthy(a.EnterpriseOrgPref("channel_email_addresses_enabled")) // !!getOrgPref(…)
}

// jsTruthy is JavaScript's !!v for a JSON value (absent is undefined).
func jsTruthy(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "", "null", "false", "0", `""`:
		return false
	}
	return true
}
