package richtext

import (
	"testing"
	"time"
)

func TestFormatDate(t *testing.T) {
	ts := time.Date(2024, 2, 18, 6, 39, 42, 0, time.UTC)
	later := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		f    string
		now  time.Time
		want string
	}{
		{"{date_num}", later, "2024-02-18"},
		{"{date_slash}", later, "02/18/2024"},
		{"{date_long}", later, "Sunday, February 18th, 2024"},
		{"{date_long_full}", later, "Sunday, February 18, 2024"},
		{"{date}", later, "February 18th, 2024"},
		{"{date_short}", later, "Feb 18, 2024"},
		{"{time}", later, "6:39 AM"},
		{"{time_secs}", later, "6:39:42 AM"},
		{"{date_short} at {time}", later, "Feb 18, 2024 at 6:39 AM"},
		{"{date_pretty}", later, "February 18th, 2024"},
		{"{date_pretty} at {time}", ts.Add(2 * time.Hour), "Today at 6:39 AM"},
		{"on {date_short_pretty}", ts.Add(24 * time.Hour), "on yesterday"},
		{"{date_long_pretty}", ts.Add(-24 * time.Hour), "Tomorrow"},
		{"{day_divider_pretty}", later, "Sunday, February 18th"},
		{"{day_divider_pretty}", later.AddDate(1, 0, 0), "Sunday, February 18th, 2024"},
		{"{day_divider_pretty}", ts, "Today"},
		{"{ago}", ts.Add(30 * time.Second), "just now"},
		{"{ago}", ts.Add(time.Minute), "1 minute ago"},
		{"{ago}", ts.Add(3 * time.Hour), "3 hours ago"},
		{"{ago}", ts.Add(-48 * time.Hour), "in 2 days"},
		{"{ago}", ts.AddDate(0, 0, 65), "2 months ago"},
		{"{ago}", ts.AddDate(3, 0, 1), "3 years ago"},
		{"{unknown} x", later, "{unknown} x"},
		{"no tokens", later, "no tokens"},
		{"broken {date", later, "broken {date"},
	}
	for _, c := range cases {
		if got := formatDate(ts, c.f, c.now); got != c.want {
			t.Errorf("formatDate(%q, now=%v) = %q, want %q", c.f, c.now, got, c.want)
		}
	}
	for d, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 31: "31st"} {
		if got := ordinal(d); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", d, got, want)
		}
	}
}

func TestSafeURL(t *testing.T) {
	cases := map[string]string{
		"https://a.test":      "https://a.test",
		" http://a.test ":     "http://a.test",
		"HTTPS://A.test":      "HTTPS://A.test",
		"mailto:a@b.test":     "mailto:a@b.test",
		"tel:+100":            "tel:+100",
		"slack://open":        "slack://open",
		"/relative":           "/relative",
		"#frag":               "#frag",
		"javascript:alert(1)": "",
		"JaVaScRiPt:alert(1)": "",
		"data:text/html,x":    "",
		"vbscript:x":          "",
		"file:///etc/passwd":  "",
		"plainword":           "",
		"":                    "",
	}
	for in, want := range cases {
		if got := SafeURL(in); got != want {
			t.Errorf("SafeURL(%q) = %q, want %q", in, got, want)
		}
	}
	img := map[string]string{
		"https://i.test/a.png": "https://i.test/a.png",
		"http://i.test/a.png":  "http://i.test/a.png",
		"/file?u=x":            "",
		"data:image/png;x":     "",
		"javascript:x":         "",
	}
	for in, want := range img {
		if got := SafeImage(in); got != want {
			t.Errorf("SafeImage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNeedsProxy(t *testing.T) {
	cases := map[string]bool{
		"https://files.slack.com/files-pri/T1-F1/a.png":     true,
		"https://files-edu.slack.com/files-pri/T1-F1/a.png": true,
		"https://FILES.slack.com/x":                         true,
		"https://ws.slack.com/files-pri/T1-F1/a.png":        true,
		"https://ws.slack.com/files-tmb/T1-F1/a.png":        true,
		"https://ws.slack.com/archives/C1":                  false,
		"https://emoji.slack-edge.com/T1/parrot/a.gif":      false,
		"https://a.slack-edge.com/x.png":                    false,
		"https://files.slack.com.evil.test/x":               false,
		"https://evil.test/files-pri/x":                     false,
		"not a url":                                         false,
		"":                                                  false,
	}
	for in, want := range cases {
		if got := NeedsProxy(in); got != want {
			t.Errorf("NeedsProxy(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFitSideAndColor(t *testing.T) {
	type wh struct{ w, h int }
	fits := []struct {
		in   [3]int
		want wh
	}{
		{[3]int{2000, 1000, 480}, wh{480, 240}},
		{[3]int{1000, 2000, 480}, wh{240, 480}},
		{[3]int{100, 50, 480}, wh{100, 50}},
		{[3]int{0, 0, 480}, wh{0, 0}},
		{[3]int{3000, 1, 720}, wh{720, 1}},
	}
	for _, f := range fits {
		w, h := fitSide(f.in[0], f.in[1], f.in[2])
		if (wh{w, h}) != f.want {
			t.Errorf("fitSide(%v) = %d,%d want %v", f.in, w, h, f.want)
		}
	}
	colors := map[string]string{"good": "#2EB886", "warning": "#DAA038", "danger": "#A30200", "#abc": "#ABC", "3aa3e3": "#3AA3E3", "red": "", "#12345": "", "": ""}
	for in, want := range colors {
		if got := attachmentColor(in); got != want {
			t.Errorf("attachmentColor(%q) = %q, want %q", in, got, want)
		}
	}
}
