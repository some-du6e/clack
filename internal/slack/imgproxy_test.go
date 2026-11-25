package slack

import "testing"

// ImgProxyURL and AnimatedImgSrc against Slack's own getImgProxyUrl and Ac
// (client-boot d0up, build e277bbacc0bc), run in node with its imports
// stubbed (the team has no image_proxy_url): every want below is what
// that code returned.
func TestImgProxyURLMatchesSlack(t *testing.T) {
	const giphy = "https://media3.giphy.com/media/v1.Y2lkPTc5MGI3NjExYWJj/l0HlNQ03J5JxX6lva/giphy.webp?cid=790b7611abc&rid=giphy.webp&ct=g"
	const giphyQ = "https%3A%2F%2Fmedia3.giphy.com%2Fmedia%2Fv1.Y2lkPTc5MGI3NjExYWJj%2Fl0HlNQ03J5JxX6lva%2Fgiphy.webp%3Fcid%3D790b7611abc%26rid%3Dgiphy.webp%26ct%3Dg"
	// AttachmentImage's <img>: rotate, a height alone (so no size)
	att := func(off bool) ImgProxyOpts {
		return ImgProxyOpts{Height: 260, SizeForRetina: true, Rotate: true, AnimationsOff: off}
	}
	for _, c := range []struct {
		name string
		got  string
		want string
	}{
		{"animations off", ImgProxyURL(giphy, att(true)), "https://slack-imgs.com/?c=1&o1=ro.gu&url=" + giphyQ},
		{"animations on", ImgProxyURL(giphy, att(false)), "https://slack-imgs.com/?c=1&o1=ro&url=" + giphyQ},
		{"playing (Ac)", AnimatedImgSrc(ImgProxyURL(giphy, att(true))), "https://slack-imgs.com/?c=1&o1=ro&url=" + giphyQ},
		{"Slack's CDN", ImgProxyURL("https://emoji.slack-edge.com/T1/party/abc.png", ImgProxyOpts{Rotate: true, AnimationsOff: true}), "https://emoji.slack-edge.com/T1/party/abc.png"},
		{"already proxied", ImgProxyURL("https://slack-imgs.com/?c=1&o1=ro&url=x", ImgProxyOpts{Rotate: true, AnimationsOff: true}), "https://slack-imgs.com/?c=1&o1=ro.gu&url=x"},
		{"~ * and a space", ImgProxyURL("https://example.com/a~b*c d", ImgProxyOpts{Rotate: true}), "https://slack-imgs.com/?c=1&o1=ro&url=https%3A%2F%2Fexample.com%2Fa%7Eb*c+d"},
		{"retina size", ImgProxyURL("https://example.com/x.png", ImgProxyOpts{Width: 220, Height: 260, SizeForRetina: true, Rotate: true}), "https://slack-imgs.com/?c=1&o1=wi440.he520.si.ro&url=https%3A%2F%2Fexample.com%2Fx.png"},
		{"size capped at 2048", ImgProxyURL("https://example.com/x.png", ImgProxyOpts{Width: 3000, Height: 1000, SizeForRetina: true}), "https://slack-imgs.com/?c=1&o1=wi2048.he682.si&url=https%3A%2F%2Fexample.com%2Fx.png"},
		{"http on Slack's CDN", ImgProxyURL("http://a.slack-edge.com/x.png", ImgProxyOpts{Rotate: true}), "https://slack-imgs.com/?c=1&o1=ro&url=http%3A%2F%2Fa.slack-edge.com%2Fx.png"},
		{"/img/ path", ImgProxyURL("/img/x.png", ImgProxyOpts{Rotate: true, AnimationsOff: true}), "/img/x.png"},
		{"stopAnimations on the proxy", ImgProxyURL("https://slack-imgs.com/?c=1&url=y", ImgProxyOpts{StopAnimations: true}), "https://slack-imgs.com/?c=1&url=y&o1=gu"},
		{"Ac without gu", AnimatedImgSrc("https://slack-imgs.com/?c=1&o1=ro&url=z"), "https://slack-imgs.com/?c=1&o1=ro&url=z"},
		{"Ac drops an empty o1", AnimatedImgSrc("https://slack-imgs.com/?c=1&o1=gu&url=z%7E"), "https://slack-imgs.com/?c=1&url=z%7E"},
	} {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", c.name, c.got, c.want)
		}
	}
	if got := ImgProxyURL("", ImgProxyOpts{}); got != "" {
		t.Errorf("an empty URL proxied to %q", got)
	}
}
