package richview

import rt "github.com/some-du6e/clack/internal/richtext"

// The image hit tests reach the new API only through these.

// hitLink is the link a hit image is the background of.
func hitLink(h Hit) string { return h.Link }

// extSourceURL is AttachmentMedia's externalSourceUrl.
func extSourceURL(a *rt.Attachment) string { return externalSourceURL(a) }

// extAtt is an attachment with these links.
func extAtt(title, orig, app, from string) rt.Attachment {
	return rt.Attachment{TitleLink: title, OriginalURL: orig, AppUnfurlURL: app, FromURL: from}
}
