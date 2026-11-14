package core

import (
	"context"
	"testing"
)

// chat.slugifyUrl answers any link that isn't a Slack object as a link
// slug with the server's short form of the URL (live shape 2026-11-14,
// enterprise host: {slug_type: "link", display_url}, for example.com URLs
// long and short); SlugifyURLs hands it on as a "link" Slug.
func TestSlugifyURLsLinkSlugs(t *testing.T) {
	a, f := testApp(t)
	const long = "https://example.com/docs/guides/abcdefghijabcdefghijabcdefghijabcdefghij"
	f.reply["chat.slugifyUrl"] = `{"ok":true,"slugs":{` +
		`"` + long + `":{"slug_type":"link","display_url":"example.com/docs/guides/abcdefghijab…"},` +
		`"https://example.com/short":{"slug_type":"link","display_url":"example.com/short"},` +
		`"https://example.com/nolabel":{"slug_type":"link"},` +
		`"https://example.com/odd":{"slug_type":"invalid"}}}`
	got, err := a.SlugifyURLs(context.Background(), []string{long, "https://example.com/short", "https://example.com/nolabel", "https://example.com/odd"}, "C1")
	if err != nil {
		t.Fatal(err)
	}
	if s := got[long]; s.Type != "link" || s.DisplayURL != "example.com/docs/guides/abcdefghijab…" {
		t.Errorf("long link: %+v", s)
	}
	if s := got["https://example.com/short"]; s.Type != "link" || s.DisplayURL != "example.com/short" {
		t.Errorf("short link: %+v", s)
	}
	if s, ok := got["https://example.com/nolabel"]; !ok || s.Type != "link" || s.DisplayURL != "" {
		t.Errorf("link without display_url: %+v (%v)", s, ok)
	}
	if s, ok := got["https://example.com/odd"]; ok {
		t.Errorf("an invalid answer resolved: %+v", s)
	}
	form := f.last("chat.slugifyUrl")
	if form.Get("reason") != "link-paste-slugify" || form.Get("channel") != "C1" {
		t.Errorf("form %v", form)
	}
}
