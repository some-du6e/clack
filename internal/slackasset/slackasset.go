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

// Get returns the bundled bytes for url, if it is one of these assets.
func Get(url string) ([]byte, bool) {
	if url == SlackLogoMark {
		return slackLogoMark, true
	}
	return nil, false
}

// IsSVG reports whether url is a bundled SVG asset (imgcache rasterizes
// those, and no other SVG).
func IsSVG(url string) bool { return url == SlackLogoMark }
