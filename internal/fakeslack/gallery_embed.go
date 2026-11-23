package fakeslack

// The message_embed block (Slack's MessageEmbed): a message in this
// channel drawn as a message unfurl from the client's copy, and an embed
// of a message the client doesn't hold, which draws nothing. Synthetic.

func init() { RegisterGallery("16-embed", galleryEmbed) }

func galleryEmbed(e GalleryEnv) []GalleryMessage {
	return []GalleryMessage{
		{Name: "embed/original", From: e.Users[0], Text: "The original message",
			Blocks: `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"The original message, with a "},{"type":"text","text":"bold","style":{"bold":true}},{"type":"text","text":" word and a "},{"type":"link","url":"https://example.com/notes","text":"link"},{"type":"text","text":"."}]}]}]`},
		{Name: "embed/message", Text: "An embedded message",
			Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"The message this one embeds:"}},{"type":"message_embed","team_id":"` + TeamID + `","channel":"` + GalleryChannelID + `","ts":"{gallery-ts:embed/original}"}]`},
		{Name: "embed/missing", Text: "An embed of a message the client doesn't hold",
			Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"An embed of a message nobody loaded (nothing under this line):"}},{"type":"message_embed","team_id":"` + TeamID + `","channel":"` + GalleryChannelID + `","ts":"1700000000.000001"}]`},
	}
}
