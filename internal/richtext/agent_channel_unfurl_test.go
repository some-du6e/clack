package richtext

import "testing"

// An agent session request's agent_channel_unfurl attachment (the code
// channel the agent made) is MessageKitAttachments' CodeChannelEntityCard,
// never a legacy attachment of its fallback text: an Agent slot for the
// element host (kind agent_channel_unfurl, the channel), after the
// message's other attachments.
func TestAgentChannelUnfurlCard(t *testing.T) {
	d := FromMessage(R, msg(t, `{"ts":"1763600000.000100","user":"U0SELF0001","text":"<@U0AGENT0001> fix it",
		"blocks":[{"type":"rich_text","block_id":"rq1","elements":[{"type":"rich_text_section","elements":[
			{"type":"user","user_id":"U0AGENT0001"},{"type":"text","text":" fix it"}]}]},
			{"type":"context","block_id":"ctx","elements":[{"type":"mrkdwn","text":"Started a code channel","verbatim":false}]}],
		"attachments":[{"id":1,"fallback":"Code channel","agent_channel_unfurl":{"channel_id":"C0CODE00001","channel_name":"fix-login-redirect",
			"is_private":false,"agent_bot_user_id":"U0AGENT0001","joinable_from_channel_id":"C0AGSESS001"}},
			{"id":2,"fallback":"a link","title":"A link","title_link":"https://example.com/","from_url":"https://example.com/","service_name":"Example"}]}`))
	var kinds []BlockKind
	for _, b := range d.Blocks {
		kinds = append(kinds, b.Kind)
		if b.Kind == AttachmentBlock && b.Att != nil {
			for _, tb := range b.Att.Text {
				if SpansText(tb.Spans) == "Code channel" {
					t.Errorf("the agent_channel_unfurl drew as a legacy attachment of its fallback text")
				}
			}
		}
	}
	last := d.Blocks[len(d.Blocks)-1]
	if last.Kind != Agent || len(last.Buttons) != 1 || last.Buttons[0].Kind != AgentChannelUnfurlKind || last.Buttons[0].BlockID != "C0CODE00001" {
		t.Fatalf("last block %+v, want the code channel's card slot after the link unfurl (kinds %v)", last, kinds)
	}
	if n := len(d.Blocks); n < 2 || d.Blocks[n-2].Kind != AttachmentBlock {
		t.Errorf("the link unfurl should come before the card (kinds %v)", kinds)
	}
}
