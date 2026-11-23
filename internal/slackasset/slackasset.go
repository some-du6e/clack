// Package slackasset holds images Slack's web client bundles as webpack
// assets (served from its public path on a.slack-edge.com) that Clack draws
// in their place, embedded so they never wait on the network.
//
// SlackLogoMark is the Slack system user's avatar. getMemberProfileImage
// (client-boot 6Knt, build 35b42de8b870) returns `n("n8qp")` for
// memberId "USLACK" at every size, before it reads avatar_hash or the
// profile's image_* fields; n8qp is
// `e.exports = n.p + "slack_logo_mark-a79a4bc.svg"`. The file here is that
// one (sha256 0af04ce5…c970, 883 bytes), byte-identical to the profile's
// own a.slack-edge.com/9cc0056/img/avatars/slack_system_user/
// slack_logo_mark.svg.
package slackasset

import _ "embed"

// SlackLogoMark is n8qp's URL under the web client's public path.
const SlackLogoMark = "https://a.slack-edge.com/bv1-13-br/slack_logo_mark-a79a4bc.svg"

//go:embed assets/slack_logo_mark-a79a4bc.svg
var slackLogoMark []byte

// Base is the web client's public path (a.slack-edge.com/bv1-13-br/).
const Base = "https://a.slack-edge.com/bv1-13-br/"

// The Slack Connect hub's images (build a09b625d90aa): the External tab's
// hero (wYY6 SC-Hub-Hero-v2-d9f20d5.svg, drawn at 224x188: shipped as
// rsvg-convert's 3x rendering, since its rotated rects, ellipses, strokes
// and opacity are past rasterSVG's subset), the social proof's portraits (JER7, mZDX,
// 1uBr) and logos (f2WD, Yiw7 / Ajbv for dark, AyB7, g37C), and the
// Invitations empty page's picture (cPlF group_6229).
const (
	SCHubHero        = Base + "SC-Hub-Hero-v2-d9f20d5@3x.png" // Clack's key for wYY6's svg
	SCInvitesEmpty   = Base + "group_6229_2x_360-90f61c6.png"
	SCPersonMing     = Base + "person-ming-c-9e159b7.png"
	SCPersonKyelie   = Base + "person-kyelie-b-1ae88a3.png"
	SCPersonMichael  = Base + "person-michael-w-ee7d074.png"
	SCLogoAirbnb     = Base + "airbnb-logo-eca8592.png"
	SCLogoOpenAI     = Base + "open-ai-logo-5172810.png"
	SCLogoOpenAIDark = Base + "open-ai-white-logo-5f707e6.png"
	SCLogoTime       = Base + "time-magazine-logo-eecd685.png"
	SCLogoHulu       = Base + "hulu-logo-4f54e67.png"
)

//go:embed assets/SC-Hub-Hero-v2-d9f20d5@3x.png
var scHubHero []byte

//go:embed assets/group_6229_2x_360-90f61c6.png
var scInvitesEmpty []byte

//go:embed assets/person-ming-c-9e159b7.png
var scPersonMing []byte

//go:embed assets/person-kyelie-b-1ae88a3.png
var scPersonKyelie []byte

//go:embed assets/person-michael-w-ee7d074.png
var scPersonMichael []byte

//go:embed assets/airbnb-logo-eca8592.png
var scLogoAirbnb []byte

//go:embed assets/open-ai-logo-5172810.png
var scLogoOpenAI []byte

//go:embed assets/open-ai-white-logo-5f707e6.png
var scLogoOpenAIDark []byte

//go:embed assets/time-magazine-logo-eecd685.png
var scLogoTime []byte

//go:embed assets/hulu-logo-4f54e67.png
var scLogoHulu []byte

var bundled = map[string][]byte{
	SlackLogoMark: slackLogoMark, SCHubHero: scHubHero, SCInvitesEmpty: scInvitesEmpty, SCPersonMing: scPersonMing,
	SCPersonKyelie: scPersonKyelie, SCPersonMichael: scPersonMichael, SCLogoAirbnb: scLogoAirbnb, SCLogoOpenAI: scLogoOpenAI,
	SCLogoOpenAIDark: scLogoOpenAIDark, SCLogoTime: scLogoTime, SCLogoHulu: scLogoHulu,
}

// Get returns the bundled bytes for url, if it is one of these assets.
func Get(url string) ([]byte, bool) {
	b, ok := bundled[url]
	return b, ok
}

// IsSVG reports whether url is a bundled SVG asset (imgcache rasterizes
// those, and no other SVG).
func IsSVG(url string) bool { return url == SlackLogoMark }
