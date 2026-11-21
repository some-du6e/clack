package fakeslack

import "fmt"

// Block Kit Builder's default template (app.slack.com/block-kit-builder,
// the restaurant picker) the way its Send to Slack button posts it: an
// ordinary message from the person using the builder, here the viewer
// (user, text, ts, type and blocks; no bot_id, app_id, bot_profile,
// subtype or client_msg_id). No app is behind its buttons. Its blocks are
// the template's, with the block_id and action_id the server fills in and
// the text escaped as the server stores it (&amp;); its three photos are on
// a third-party https host, which Clack loads through Slack's image proxy
// (ProxiedImageURL: the photos' own host answers 404). The template is
// Slack's public example, so nothing here is anyone's message.

// GalleryBuilderPreview is the gallery name of the posted template.
const GalleryBuilderPreview = "builder/preview"

// The template's photos (the fake's pictures, at the originals' sizes).
var (
	GalleryBuilderPhotos = []string{
		ProxiedImageURL("https://s3-media3.fl.yelpcdn.com", 1000, 666, "farmhouse"),
		ProxiedImageURL("https://s3-media2.fl.yelpcdn.com", 666, 1000, "kin-khao"),
		ProxiedImageURL("https://s3-media2.fl.yelpcdn.com", 1000, 750, "ler-ros"),
	}
)

func init() { RegisterGallery("zz-builder", galleryBuilder) }

func galleryBuilder(GalleryEnv) []GalleryMessage {
	p := GalleryBuilderPhotos
	return []GalleryMessage{{Name: GalleryBuilderPreview, From: SelfID, Blocks: fmt.Sprintf(builderTemplate, p[0], p[1], p[2])}}
}

const builderTemplate = `[
	{"type": "section", "block_id": "Jq7Tz", "text": {"type": "mrkdwn", "text": "Hello, Assistant to the Regional Manager Dwight! *Michael Scott* wants to know where you'd like to take the Paper Company investors to dinner tonight.\n\n *Please select a restaurant:*", "verbatim": false}},
	{"type": "divider", "block_id": "pV0dK"},
	{"type": "section", "block_id": "u8RxN", "text": {"type": "mrkdwn", "text": "*Farmhouse Thai Cuisine*\n:star::star::star::star: 1528 reviews\n They do have some vegan options, like the roti and curry, plus they have a ton of salad stuff and noodles can be ordered without meat!! They have something for everyone here", "verbatim": false},
		"accessory": {"type": "image", "image_url": %q, "alt_text": "alt text for image"}},
	{"type": "section", "block_id": "C3mfa", "text": {"type": "mrkdwn", "text": "*Kin Khao*\n:star::star::star::star: 1638 reviews\n The sticky rice also goes wonderfully with the caramelized pork belly, which is absolutely melt-in-your-mouth and so soft.", "verbatim": false},
		"accessory": {"type": "image", "image_url": %q, "alt_text": "alt text for image"}},
	{"type": "section", "block_id": "y1HbE", "text": {"type": "mrkdwn", "text": "*Ler Ros*\n:star::star::star::star: 2082 reviews\n I would really recommend the  Yum Koh Moo Yang - Spicy lime dressing and roasted quick marinated pork shoulder, basil leaves, chili &amp; rice powder.", "verbatim": false},
		"accessory": {"type": "image", "image_url": %q, "alt_text": "alt text for image"}},
	{"type": "divider", "block_id": "Rt6wQ"},
	{"type": "actions", "block_id": "nO4gS", "elements": [
		{"type": "button", "action_id": "a1Zx8", "text": {"type": "plain_text", "text": "Farmhouse", "emoji": true}, "value": "click_me_123"},
		{"type": "button", "action_id": "Bv4Lm", "text": {"type": "plain_text", "text": "Kin Khao", "emoji": true}, "value": "click_me_123", "url": "https://google.com"},
		{"type": "button", "action_id": "q9TcW", "text": {"type": "plain_text", "text": "Ler Ros", "emoji": true}, "value": "click_me_123", "url": "https://google.com"}
	]}
]`
