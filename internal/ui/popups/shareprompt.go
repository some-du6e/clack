package popups

// A share_prompt's Send to {agentName} (Slack's SharePromptButton, its
// onClick eb, then startAgentOrAssistantThread, boot-shared G2io; CDN
// build 9f450b8ad3db): the prompt goes to the agent's DM and that
// message's thread opens. When the send fails, openDeadEndLinkDialog
// (4tRg) shows DeadEndLinkDialog (aWKL) with fallbackLinkType
// "shared_prompt".

import (
	"context"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

var nsDeadEnd = i18n.NS("private_message_forwarding")

// SharePromptSend posts p's prompt to its agent's DM (core's
// SendSharedPrompt) and opens the thread of the message it sent.
func SharePromptSend(h Host, p *richtext.SharePrompt) {
	if p == nil || p.BotUserID == "" {
		return
	}
	a := h.Core()
	if a == nil {
		return
	}
	type sent struct{ ch, ts string }
	run(h, func(ctx context.Context) (sent, error) {
		ch, ts, err := a.SendSharedPrompt(ctx, p.BotUserID, p.Prompt, p.RichRaw)
		return sent{ch, ts}, err
	}, func(s sent, err error) {
		if err != nil || s.ch == "" || s.ts == "" {
			h.ShowOverlay(NewDeadEndLinkDialog(h, "shared_prompt"))
			return
		}
		h.OpenThread(s.ch, s.ts)
	})
}

// NewDeadEndLinkDialog is DeadEndLinkDialog: "We’re unable to open this
// link", the sentence for the link's type and Ok (danger), which closes
// it.
func NewDeadEndLinkDialog(h Host, linkType string) *Modal {
	v := richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: deadEndText(linkType)}}}}}, richview.Options{NoHoverCards: true})
	return NewModal(h, ModalOpts{Title: nsDeadEnd.T("We’re unable to open this link"), Body: func(gtx layout.Context) layout.Dimensions {
		if RichStyle == nil {
			return layout.Dimensions{}
		}
		st := RichStyle(h)
		if st == nil {
			return layout.Dimensions{}
		}
		return v.Layout(gtx, st, topicHandler{h})
	}, Buttons: []*ModalButton{{Label: nsDeadEnd.T("Ok"), Kind: BtnDanger, Do: func() bool { return true }}}})
}

// deadEndText is DeadEndLinkDialog's sentence for a link of type t.
func deadEndText(t string) string {
	switch t {
	case "canvas":
		return nsDeadEnd.T("This link opens a canvas that you don’t have access to, or that’s been deleted.")
	case "create":
		return nsDeadEnd.T("This link opens a template that you don’t have access to, or that’s been deleted.")
	case "shared_prompt":
		return nsDeadEnd.T("This link opens an agent that you don’t have access to, or something else went wrong.")
	case "channel-section":
		return nsDeadEnd.T("This link opens a channel section that you don’t have access to, or something else went wrong.")
	}
	return nsDeadEnd.T("This link opens a message or conversation that you don’t have access to, or that’s been deleted.")
}
