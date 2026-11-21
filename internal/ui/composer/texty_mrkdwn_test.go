package composer

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// oracleNames is the store the oracle cases ran with
// (/root/clack-ops/scratch/topic-edit-mentions/harness/cases.json).
type oracleNames struct{}

func (oracleNames) User(id string) (*slack.User, bool) {
	switch id {
	case "U0FAKE00003":
		return &slack.User{ID: id, RealName: "Kari Mo", Profile: slack.Profile{DisplayName: "kari.mo", RealName: "Kari Mo"}}, true
	case "U0FAKE00004":
		return &slack.User{ID: id, RealName: "Real Only", Profile: slack.Profile{RealName: "Real Only"}}, true
	case "W0FAKE00005":
		return &slack.User{ID: id}, true
	}
	return nil, false
}

func (oracleNames) ChannelName(id string) string {
	switch id {
	case "C0FAKE00001":
		return "zumo-ka"
	case "G0FAKE00002":
		return "secret-room"
	}
	return id
}

func (oracleNames) UsergroupName(id string) string {
	if id == "S0FAKE00001" {
		return "eng-team"
	}
	return id
}

func (oracleNames) UsergroupsLoaded() bool { return true }

func oracleEmoji(name string) (*Atom, bool) {
	switch name {
	case "smile", "tada", "slightly_smiling_face", "heart", "+1":
		return &Atom{Kind: AtomEmoji, ID: name, Text: ":" + name + ":"}, true
	}
	return nil, false
}

// oracleShown is the input's text as the oracle prints it: each emoji
// "[:name:]".
func oracleShown(d *Doc) string {
	var b strings.Builder
	for i, r := range d.Runes {
		if a := d.Attrs[i].Atom; a != nil {
			if a.Kind == AtomEmoji {
				b.WriteString("[:" + a.ID + ":]")
			} else {
				b.WriteString(a.Text)
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// A stored topic or description in Slack's Texty input and back, against
// Slack's own modules run in node (harness/oracle.cjs over the CDN chunks
// of boot-shared 13b0d600b14fab13): message_to_contents_string (L9Xf) and
// convertContentsStringToContents (rKd0) give what the input shows,
// convertContentsToString (QgaI) its untouched value(), and
// linkifyPhoneNumbers (cyvU) of clean_message_text_with_state (DfFm) what
// submitChannelTopic sends, for the value as loaded and with " more"
// typed after it. convert_emoticons is on.
func TestTextyMrkdwnSlackOracle(t *testing.T) {
	cases := []struct{ in, shown, value, sent, more string }{
		{in: "The Hack Club Alums Society (UNCs Channel)\nPrivate channel: <#C0B5XH84KCZ>",
			shown: "The Hack Club Alums Society (UNCs Channel)\nPrivate channel: #C0B5XH84KCZ",
			value: "The Hack Club Alums Society (UNCs Channel)\nPrivate channel: <#C0B5XH84KCZ|#C0B5XH84KCZ>",
			sent:  "The Hack Club Alums Society (UNCs Channel)\nPrivate channel: <#C0B5XH84KCZ|C0B5XH84KCZ>",
			more:  "The Hack Club Alums Society (UNCs Channel)\nPrivate channel: <#C0B5XH84KCZ|C0B5XH84KCZ> more"},
		{in: "Member <#C0FAKE00001|old-name> and <#C0FAKE00001> and private <#G0FAKE00002> and hidden <#C0B5XH84KCZ|alumni>",
			shown: "Member #zumo-ka and #zumo-ka and private #secret-room and hidden #C0B5XH84KCZ",
			value: "Member <#C0FAKE00001|#zumo-ka> and <#C0FAKE00001|#zumo-ka> and private <#G0FAKE00002|#secret-room> and hidden <#C0B5XH84KCZ|#C0B5XH84KCZ>",
			sent:  "Member <#C0FAKE00001|zumo-ka> and <#C0FAKE00001|zumo-ka> and private <#G0FAKE00002|secret-room> and hidden <#C0B5XH84KCZ|C0B5XH84KCZ>",
			more:  "Member <#C0FAKE00001|zumo-ka> and <#C0FAKE00001|zumo-ka> and private <#G0FAKE00002|secret-room> and hidden <#C0B5XH84KCZ|C0B5XH84KCZ> more"},
		{in: "Ask <@U0FAKE00003> or <@U0FAKE00003|kari> or <@U0FAKE00004> or <@W0FAKE00005> or <@U0UNKNOWN99>",
			shown: "Ask @kari.mo or @kari.mo or @Real Only or @W0FAKE00005 or @U0UNKNOWN99",
			value: "Ask <@U0FAKE00003|@kari.mo> or <@U0FAKE00003|@kari.mo> or <@U0FAKE00004|@Real Only> or <@W0FAKE00005|@W0FAKE00005> or <@U0UNKNOWN99|@U0UNKNOWN99>",
			sent:  "Ask <@U0FAKE00003> or <@U0FAKE00003> or <@U0FAKE00004> or <@W0FAKE00005> or <@U0UNKNOWN99>",
			more:  "Ask <@U0FAKE00003> or <@U0FAKE00003> or <@U0FAKE00004> or <@W0FAKE00005> or <@U0UNKNOWN99> more"},
		{in: "Ping <!here> <!channel> <!everyone> <!here|here> <!group> <!subteam^S0FAKE00001|@eng> <!subteam^S0FAKE00001> <!subteam^S0NOPE0001|@gone> <!subteam^S0NOPE0002>",
			shown: "Ping @here @channel @everyone @here @group @eng-team @eng-team @gone @unknown-group",
			value: "Ping <@BKhere|@here> <@BKchannel|@channel> <@BKeveryone|@everyone> <@BKhere|@here> <@BKgroup|@group> <@S0FAKE00001|@eng-team> <@S0FAKE00001|@eng-team> <@S0NOPE0001|@gone> <@S0NOPE0002|@unknown-group>",
			sent:  "Ping <!here> <!channel> <!everyone> <!here> <!group> <!subteam^S0FAKE00001|@eng-team> <!subteam^S0FAKE00001|@eng-team> <!subteam^S0NOPE0001|@gone> <!subteam^S0NOPE0002|@unknown-group>",
			more:  "Ping <!here> <!channel> <!everyone> <!here> <!group> <!subteam^S0FAKE00001|@eng-team> <!subteam^S0FAKE00001|@eng-team> <!subteam^S0NOPE0001|@gone> <!subteam^S0NOPE0002|@unknown-group> more"},
		{in: "Docs: <https://example.com/docs|the docs> and <https://x.example.com/z> and <mailto:a@b.co|mail me> and <http://e.com|e.com>",
			shown: "Docs: the docs and https://x.example.com/z and mail me and e.com",
			value: "Docs: the docs and https://x.example.com/z and mail me and e.com",
			sent:  "Docs: the docs and https://x.example.com/z and mail me and e.com",
			more:  "Docs: the docs and https://x.example.com/z and mail me and e.com more"},
		{in: "Fish &amp; chips &lt;b&gt; &quot;q&quot; &#39;s",
			shown: "Fish & chips <b> \"q\" 's",
			value: "Fish & chips <b> \"q\" 's",
			sent:  "Fish &amp; chips &lt;b&gt; \"q\" 's",
			more:  "Fish &amp; chips &lt;b&gt; \"q\" 's more"},
		{in: "code `<@U0FAKE00003>` and ```<#C0FAKE00001> pre``` after",
			shown: "code `@kari.mo` and ```#zumo-ka pre``` after",
			value: "code `<@U0FAKE00003|@kari.mo>` and ```<#C0FAKE00001|#zumo-ka> pre``` after",
			sent:  "code `@kari.mo` and ```#zumo-ka pre``` after",
			more:  "code `@kari.mo` and ```#zumo-ka pre``` after more"},
		{in: "emoji :smile: :nope: `:tada:` https://e.com/:tada: :tada::skin-tone-2:",
			shown: "emoji [:smile:] :nope: `:tada:` https://e.com/:tada: :tada::skin-tone-2:",
			value: "emoji :smile: :nope: `:tada:` https://e.com/:tada: :tada::skin-tone-2:",
			sent:  "emoji :smile: :nope: `:tada:` https://e.com/:tada: :tada::skin-tone-2:",
			more:  "emoji :smile: :nope: `:tada:` https://e.com/:tada: :tada::skin-tone-2: more"},
		{in: "  padded \u200btext\u200b  \n  second line  ",
			shown: "padded \u200btext\u200b  \n  second line",
			value: "padded \u200btext\u200b  \n  second line",
			sent:  "padded \u200btext\n  second line",
			more:  "padded \u200btext\n  second line more"},
		{in: "Call 555-123-4567 or (555) 123-4567 or +44 20 7946 0958 now",
			shown: "Call 555-123-4567 or (555) 123-4567 or +44 20 7946 0958 now",
			value: "Call 555-123-4567 or (555) 123-4567 or +44 20 7946 0958 now",
			sent:  "Call <tel:5551234567|555-123-4567> or <tel:5551234567|(555) 123-4567> or <tel:+442079460958|+44 20 7946 0958> now",
			more:  "Call <tel:5551234567|555-123-4567> or <tel:5551234567|(555) 123-4567> or <tel:+442079460958|+44 20 7946 0958> now more"},
		{in: "Smile :) and (wink ;) ) and 8) list\n8) eight",
			shown: "Smile :) and (wink ;) ) and 8) list\n8) eight",
			value: "Smile :) and (wink ;) ) and 8) list\n8) eight",
			sent:  "Smile :slightly_smiling_face: and (wink :wink: ) and :sunglasses: list\n:sunglasses: eight",
			more:  "Smile :slightly_smiling_face: and (wink :wink: ) and :sunglasses: list\n:sunglasses: eight more"},
		{in: "A <#C0FAKE00001|zumo-ka> line\n> quote :)\n>:( angry",
			shown: "A #zumo-ka line\n> quote :)\n>:( angry",
			value: "A <#C0FAKE00001|#zumo-ka> line\n> quote :)\n>:( angry",
			sent:  "A <#C0FAKE00001|zumo-ka> line\n&gt; quote :)\n&gt;:( angry",
			more:  "A <#C0FAKE00001|zumo-ka> line\n&gt; quote :)\n&gt;:( angry more"},
		{in: "unknown <%L0SECTION1|%label> <@T0TEAM00001|%team> bad <#C01|#short>",
			shown: "unknown label @T0TEAM00001 bad <#C01|#C01>",
			value: "unknown <%L0SECTION1|%label> <%T0TEAM00001|%@T0TEAM00001> bad <#C01|#C01>",
			sent:  "unknown <@L0SECTION1> <@T0TEAM00001> bad &lt;#C01|#C01&gt;",
			more:  "unknown <@L0SECTION1> <@T0TEAM00001> bad &lt;#C01|#C01&gt; more"},
		{in: "",
			shown: "",
			value: "",
			sent:  "",
			more:  "more"},
		// phone numbers inside bare-domain URLs and e-mail addresses stay text
		// (detectLinks merges detectUrls and detectEmails before the phones)
		{in: "zoom.us/j/(555-123-4567) ok",
			shown: "zoom.us/j/(555-123-4567) ok",
			value: "zoom.us/j/(555-123-4567) ok",
			sent:  "zoom.us/j/(555-123-4567) ok",
			more:  "zoom.us/j/(555-123-4567) ok more"},
		{in: "x e.co.uk/(555-123-4567) ok",
			shown: "x e.co.uk/(555-123-4567) ok",
			value: "x e.co.uk/(555-123-4567) ok",
			sent:  "x e.co.uk/(555-123-4567) ok",
			more:  "x e.co.uk/(555-123-4567) ok more"},
		{in: "mail 555.123.4567.home@corp.com",
			shown: "mail 555.123.4567.home@corp.com",
			value: "mail 555.123.4567.home@corp.com",
			sent:  "mail 555.123.4567.home@corp.com",
			more:  "mail 555.123.4567.home@corp.com more"},
		{in: "mail 555-123-4567.x@e.com now",
			shown: "mail 555-123-4567.x@e.com now",
			value: "mail 555-123-4567.x@e.com now",
			sent:  "mail 555-123-4567.x@e.com now",
			more:  "mail 555-123-4567.x@e.com now more"},
		{in: "see example.com/?q='555-123-4567'",
			shown: "see example.com/?q='555-123-4567'",
			value: "see example.com/?q='555-123-4567'",
			sent:  "see example.com/?q='555-123-4567'",
			more:  "see example.com/?q='555-123-4567' more"},
		{in: "Call 555-123-4567.com",
			shown: "Call 555-123-4567.com",
			value: "Call 555-123-4567.com",
			sent:  "Call 555-123-4567.com",
			more:  "Call 555-123-4567.com more"},
		{in: "foo.bar/(555-123-4567)",
			shown: "foo.bar/(555-123-4567)",
			value: "foo.bar/(555-123-4567)",
			sent:  "foo.bar/(<tel:5551234567|555-123-4567>)",
			more:  "foo.bar/(<tel:5551234567|555-123-4567>) more"},
		{in: "call (555) 123-4567 at acme.com",
			shown: "call (555) 123-4567 at acme.com",
			value: "call (555) 123-4567 at acme.com",
			sent:  "call <tel:5551234567|(555) 123-4567> at acme.com",
			more:  "call <tel:5551234567|(555) 123-4567> at acme.com more"},
	}
	for _, c := range cases {
		d := MrkdwnTextyDoc(oracleNames{}, c.in, oracleEmoji, false)
		if got := oracleShown(d); got != c.shown {
			t.Errorf("%q shows\n  %q, want\n  %q", c.in, got, c.shown)
		}
		v := TextyValue(d)
		if v != c.value {
			t.Errorf("%q value\n  %q, want\n  %q", c.in, v, c.value)
		}
		if got := LinkifyPhoneNumbers(CleanMessageText(v, true)); got != c.sent {
			t.Errorf("%q sends\n  %q, want\n  %q", c.in, got, c.sent)
		}
		if got := LinkifyPhoneNumbers(CleanMessageText(v+" more", true)); got != c.more {
			t.Errorf("%q edited sends\n  %q, want\n  %q", c.in, got, c.more)
		}
	}
}

type linkWant struct {
	kind string
	at   int
	text string
}

// detectLinks (nVll: detectUrls 7Jnr, detectEmails hlZ9, then the phones
// of L9Ir, merged with a0ov) and linkifyPhoneNumbers (cyvU) against
// Slack's own modules (harness/links.cjs over links_cases.json, lodash
// some and inRange real): every match's kind, offset and text, and what
// cyvU sends.
func TestTextyLinksSlackOracle(t *testing.T) {
	cases := []struct {
		in    string
		links []linkWant
		sent  string
	}{
		{in: "zoom.us/j/(555-123-4567) ok",
			links: []linkWant{{"url", 0, "zoom.us/j/(555-123-4567)"}},
			sent:  "zoom.us/j/(555-123-4567) ok"},
		{in: "x e.co.uk/(555-123-4567) ok",
			links: []linkWant{{"url", 2, "e.co.uk/(555-123-4567)"}},
			sent:  "x e.co.uk/(555-123-4567) ok"},
		{in: "mail 555.123.4567.home@corp.com",
			links: []linkWant{{"email", 5, "555.123.4567.home@corp.com"}},
			sent:  "mail 555.123.4567.home@corp.com"},
		{in: "mail 555-123-4567.x@e.com now",
			links: []linkWant{{"email", 5, "555-123-4567.x@e.com"}},
			sent:  "mail 555-123-4567.x@e.com now"},
		{in: "see example.com/?q='555-123-4567'",
			links: []linkWant{{"url", 4, "example.com/?q='555-123-4567'"}},
			sent:  "see example.com/?q='555-123-4567'"},
		{in: "Call 555-123-4567.com",
			links: []linkWant{{"url", 5, "555-123-4567.com"}},
			sent:  "Call 555-123-4567.com"},
		{in: "foo.bar/(555-123-4567)",
			links: []linkWant{{"phone", 9, "555-123-4567"}},
			sent:  "foo.bar/(<tel:5551234567|555-123-4567>)"},
		{in: "call (555) 123-4567 at acme.com",
			links: []linkWant{{"phone", 5, "(555) 123-4567"}, {"url", 23, "acme.com"}},
			sent:  "call <tel:5551234567|(555) 123-4567> at acme.com"},
		{in: "see https://en.wikipedia.org/wiki/X_(555-123-4567) ok",
			links: []linkWant{{"url", 4, "https://en.wikipedia.org/wiki/X_(555-123-4567)"}},
			sent:  "see https://en.wikipedia.org/wiki/X_(555-123-4567) ok"},
		{in: "www.example.org/(555-123-4567) x",
			links: []linkWant{{"url", 0, "www.example.org/(555-123-4567)"}},
			sent:  "www.example.org/(555-123-4567) x"},
		{in: "ftp://files.example.net/(555-123-4567)",
			links: []linkWant{{"url", 0, "ftp://files.example.net/(555-123-4567)"}},
			sent:  "ftp://files.example.net/(555-123-4567)"},
		{in: "mailto:555-123-4567@x.com",
			links: []linkWant{{"email", 7, "555-123-4567@x.com"}},
			sent:  "mailto:555-123-4567@x.com"},
		{in: "_a.b@example.com and *c@d.io",
			links: []linkWant{{"email", 1, "a.b@example.com"}, {"email", 22, "c@d.io"}},
			sent:  "_a.b@example.com and *c@d.io"},
		{in: "git@github.com:org/repo and 555-123-4567",
			links: []linkWant{{"phone", 28, "555-123-4567"}},
			sent:  "git@github.com:org/repo and <tel:5551234567|555-123-4567>"},
		{in: "a@b.zz 555-123-4567",
			links: []linkWant{{"phone", 7, "555-123-4567"}},
			sent:  "a@b.zz <tel:5551234567|555-123-4567>"},
		{in: "visit Example.CO or foo.sh or bar.SH now",
			links: []linkWant{{"url", 6, "Example.CO"}},
			sent:  "visit Example.CO or foo.sh or bar.SH now"},
		{in: "http://x.com|(555) 123-4567",
			links: []linkWant{{"url", 0, "http://x.com"}},
			sent:  "http://x.com|(555) 123-4567"},
		{in: "(see http://a.com/b) and [https://c.dev/d]",
			links: []linkWant{{"url", 5, "http://a.com/b"}, {"url", 26, "https://c.dev/d"}},
			sent:  "(see http://a.com/b) and [https://c.dev/d]"},
		{in: "javascript:alert(1) and vbscript:x",
			links: []linkWant{},
			sent:  "javascript:alert(1) and vbscript:x"},
		{in: "spotify:track:abc123 rdar://12345&6 daylite://Command=ShowObject&x",
			links: []linkWant{{"url", 21, "rdar://12345&6"}, {"url", 36, "daylite://Command=ShowObject&x"}},
			sent:  "spotify:track:abc123 rdar://12345&6 daylite://Command=ShowObject&x"},
		{in: "host.local:8080/path, x.com:443/a?b#c.",
			links: []linkWant{{"url", 22, "x.com:443/a?b#c"}},
			sent:  "host.local:8080/path, x.com:443/a?b#c."},
		{in: "foo..bar.com/x and a.com..",
			links: []linkWant{{"url", 5, "bar.com/x"}, {"url", 19, "a.com"}},
			sent:  "foo..bar.com/x and a.com.."},
		{in: "_under.com and x_y.com/_z",
			links: []linkWant{{"url", 1, "under.com"}, {"url", 15, "x_y.com/_z"}},
			sent:  "_under.com and x_y.com/_z"},
		{in: "cafe.co_uk and a.b.c.d.com/x",
			links: []linkWant{{"url", 15, "a.b.c.d.com/x"}},
			sent:  "cafe.co_uk and a.b.c.d.com/x"},
		{in: "u@x.com/555-123-4567",
			links: []linkWant{{"email", 0, "u@x.com"}},
			sent:  "u@x.com/555-123-4567"},
		{in: "1.2.3.4/555-123-4567 and 10.0.0.1",
			links: []linkWant{},
			sent:  "1.2.3.4/555-123-4567 and 10.0.0.1"},
		{in: "https://\u4f8b\u3048.jp/(555-123-4567) \u6771\u4eacexample.com\u6771\u4eac",
			links: []linkWant{{"url", 0, "https://\u4f8b\u3048.jp/(555-123-4567)"}},
			sent:  "https://\u4f8b\u3048.jp/(555-123-4567) \u6771\u4eacexample.com\u6771\u4eac"},
		{in: "call +44 20 7946 0958 or 555-123-4567.",
			links: []linkWant{{"phone", 5, "+44 20 7946 0958"}, {"phone", 25, "555-123-4567"}},
			sent:  "call <tel:+442079460958|+44 20 7946 0958> or <tel:5551234567|555-123-4567>."},
		{in: "tel 555 123 4567 at x.io/ok",
			links: []linkWant{{"phone", 4, "555 123 4567"}, {"url", 20, "x.io/ok"}},
			sent:  "tel <tel:5551234567|555 123 4567> at x.io/ok"},
		{in: "x.now and ms.now/a and y.zip",
			links: []linkWant{{"url", 10, "ms.now/a"}},
			sent:  "x.now and ms.now/a and y.zip"},
		{in: "//www.x.com/(555-123-4567) //y.com/(555-123-4567)",
			links: []linkWant{{"url", 0, "//www.x.com/(555-123-4567)"}, {"url", 27, "//y.com/(555-123-4567)"}},
			sent:  "//www.x.com/(555-123-4567) //y.com/(555-123-4567)"},
		{in: "go to https://x.com/a'b' and https://y.com/c'",
			links: []linkWant{{"url", 6, "https://x.com/a'b'"}, {"url", 29, "https://y.com/c"}},
			sent:  "go to https://x.com/a'b' and https://y.com/c'"},
	}
	for _, c := range cases {
		var got []linkWant
		for _, m := range detectLinks(c.in) {
			got = append(got, linkWant{m.kind, m.at, m.text})
		}
		if len(got) != len(c.links) {
			t.Errorf("%q links %v, want %v", c.in, got, c.links)
		} else {
			for i := range got {
				if got[i] != c.links[i] {
					t.Errorf("%q link %d %v, want %v", c.in, i, got[i], c.links[i])
				}
			}
		}
		if s := LinkifyPhoneNumbers(c.in); s != c.sent {
			t.Errorf("%q sends\n  %q, want\n  %q", c.in, s, c.sent)
		}
	}
}
