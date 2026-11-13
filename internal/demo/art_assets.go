package demo

import (
	"hash/fnv"
	"strings"
)

// Slack's own static images (a.slack-edge.com: the Huddles page's hero
// and backgrounds, Slackbot's and the sample avatars in Preferences).
// The demo is offline and fetches none of them: SlackAsset draws a
// stand-in of the same kind so the page isn't left with holes.

// slackAssetHost is where Slack's web client keeps its static images.
const slackAssetHost = "https://a.slack-edge.com/"

// SlackAsset draws a stand-in for one of Slack's static images (false:
// not one).
func SlackAsset(raw string) ([]byte, bool) {
	if !strings.HasPrefix(raw, slackAssetHost) {
		return nil, false
	}
	p := strings.ToLower(raw)
	switch {
	case strings.Contains(p, "slackbot"):
		return cached("asset/slackbot", func() []byte { return drawSlackbot(144) }), true
	case strings.Contains(p, "avatar") || strings.Contains(p, "/img/avatars"):
		h := fnv.New32a()
		h.Write([]byte(p))
		styles := []string{"short", "long", "curly", "bun", "bob"}
		bgs := []string{"#7c9cbf", "#c9a26b", "#8fb996", "#b98fb1"}
		n := int(h.Sum32())
		a := Avatar{Bg: bgs[n%len(bgs)], Skin: []string{"#e8b48a", "#c68642", "#8d5524"}[n%3], Hair: "#3b2a20", Style: styles[n%len(styles)]}
		return cached("asset/"+p, func() []byte { return drawAvatar(a, 144) }), true
	case strings.Contains(p, "huddle") || strings.HasSuffix(p, ".jpg") || strings.HasSuffix(p, ".jpeg"):
		return cached("asset/"+p, func() []byte { return drawScene(p, 800, 450) }), true
	}
	return cached("asset/"+p, func() []byte { return drawScene(p, 400, 300) }), true
}

// drawSlackbot is a friendly robot head on Slack's teal.
func drawSlackbot(px int) []byte {
	c := newCV(px, px, float64(px)/100)
	gfill(c, 0, 0, 0, 100, hex("#3fc6c1"), hex("#239e9a"), sh().rect(0, 0, 100, 100))
	c.fill(white, sh().rrect(22, 30, 56, 46, 14))
	c.fill(white, sh().rect(48, 18, 4, 14))
	c.fill(hex("#ecb22e"), sh().circle(50, 17, 5))
	c.fill(hex("#1d1c1d"), sh().circle(38, 50, 6).circle(62, 50, 6))
	c.fill(hex("#1d1c1d"), sh().arc(50, 58, 10, 0.4, 2.74, 3))
	return c.png()
}

// drawScene is a soft landscape whose colours follow the asset's name:
// the huddle backgrounds and the page's hero.
func drawScene(key string, w, h int) []byte {
	hs := fnv.New32a()
	hs.Write([]byte(key))
	n := hs.Sum32()
	skies := [][2]string{{"#ffd3a5", "#fd6585"}, {"#a1c4fd", "#c2e9fb"}, {"#2e1760", "#5b3ea8"}, {"#d4fc79", "#96e6a1"}, {"#fbc2eb", "#a6c1ee"}}
	sky := skies[n%uint32(len(skies))]
	k := float64(w) / 1000
	c := newCV(w, h, k)
	H := float64(h) / k
	gfill(c, 0, 0, 0, H, hex(sky[0]), hex(sky[1]), sh().rect(0, 0, 1000, H))
	c.fill(alpha(white, 0.75), sh().circle(220+float64(n%500), H*0.3, 60))
	far, near := shade(hex(sky[1]), -0.25), shade(hex(sky[1]), -0.45)
	c.fill(far, sh().blob(0, H*0.7, 200, H*0.5, 420, H*0.62, 640, H*0.45, 860, H*0.6, 1000, H*0.55, 1000, H, 0, H))
	c.fill(near, sh().blob(0, H*0.85, 260, H*0.72, 520, H*0.82, 780, H*0.7, 1000, H*0.8, 1000, H, 0, H))
	return c.png()
}
