package richtext

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SafeURL returns u (trimmed) if its scheme is safe to open, else "".
// Allowed: http, https, mailto, tel, sms, ftp, slack, and relative paths.
func SafeURL(u string) string {
	t := strings.TrimSpace(u)
	if t == "" {
		return ""
	}
	i := strings.IndexByte(t, ':')
	if i < 0 {
		if strings.HasPrefix(t, "/") || strings.HasPrefix(t, "#") {
			return t
		}
		return ""
	}
	switch strings.ToLower(t[:i]) {
	case "http", "https", "mailto", "tel", "slack", "ftp", "sms":
		return t
	}
	return ""
}

// SafeImage returns u if it is an absolute http(s) URL, else "".
func SafeImage(u string) string {
	t := strings.TrimSpace(u)
	l := strings.ToLower(t)
	if strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") {
		return t
	}
	return ""
}

// NeedsProxy reports whether u is an authenticated Slack file URL that must
// be fetched with the session cookie (core.Fetch), e.g. files.slack.com.
func NeedsProxy(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Host == "" {
		return false
	}
	h := strings.ToLower(p.Hostname())
	if h == "files.slack.com" || (strings.HasPrefix(h, "files") && strings.HasSuffix(h, ".slack.com")) {
		return true
	}
	return strings.HasSuffix(h, ".slack.com") && (strings.HasPrefix(p.Path, "/files-pri/") || strings.HasPrefix(p.Path, "/files-tmb/") || strings.HasPrefix(p.Path, "/files/"))
}

// image builds an *Image for a safe http(s) URL, or nil.
func image(u, alt string, w, h int) *Image {
	s := SafeImage(u)
	if s == "" {
		return nil
	}
	return &Image{URL: s, Alt: alt, W: w, H: h, Proxy: NeedsProxy(s)}
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// FormatDate renders Slack's date tokens ({date_num}, {date_slash},
// {date_long}, {date_long_full}, {date_long_pretty}, {date}, {date_pretty},
// {date_short}, {date_short_pretty}, {day_divider_pretty}, {time},
// {time_secs}, {ago}) in t's location, relative to now for the pretty and
// {ago} forms. Unknown tokens are left as is.
func FormatDate(t time.Time, format string) string {
	return formatDate(t, format, time.Now().In(t.Location()))
}

func formatDate(t time.Time, f string, now time.Time) string {
	if !strings.Contains(f, "{") {
		return f
	}
	o := t.Format("January ") + ordinal(t.Day())
	long := t.Format("Monday, ") + o + t.Format(", 2006")
	date := o + t.Format(", 2006")
	short := t.Format("Jan 2, 2006")
	rel := relativeDay(t, now)
	var b strings.Builder
	for i := 0; i < len(f); {
		if f[i] != '{' {
			b.WriteByte(f[i])
			i++
			continue
		}
		j := strings.IndexByte(f[i:], '}')
		if j < 0 {
			b.WriteString(f[i:])
			break
		}
		tok := f[i+1 : i+j]
		start := i == 0
		i += j + 1
		pretty := func(fallback string) string {
			if rel == "" {
				return fallback
			}
			if start {
				return strings.ToUpper(rel[:1]) + rel[1:]
			}
			return rel
		}
		switch tok {
		case "date_num":
			b.WriteString(t.Format("2006-01-02"))
		case "date_slash":
			b.WriteString(t.Format("01/02/2006"))
		case "date_long":
			b.WriteString(long)
		case "date_long_full":
			b.WriteString(t.Format("Monday, January 2, 2006"))
		case "date_long_pretty":
			b.WriteString(pretty(long))
		case "date":
			b.WriteString(date)
		case "date_pretty":
			b.WriteString(pretty(date))
		case "date_short":
			b.WriteString(short)
		case "date_short_pretty":
			b.WriteString(pretty(short))
		case "day_divider_pretty":
			if rel != "" {
				b.WriteString(strings.ToUpper(rel[:1]) + rel[1:])
			} else if t.Year() == now.Year() {
				b.WriteString(t.Format("Monday, ") + o)
			} else {
				b.WriteString(long)
			}
		case "time":
			b.WriteString(t.Format("3:04 PM"))
		case "time_secs":
			b.WriteString(t.Format("3:04:05 PM"))
		case "ago":
			b.WriteString(ago(t, now))
		default:
			b.WriteString("{" + tok + "}")
		}
	}
	return b.String()
}

// relativeDay returns "today", "yesterday", "tomorrow" or "".
func relativeDay(t, now time.Time) string {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	a := time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC)
	b := time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC)
	switch int(a.Sub(b).Hours() / 24) {
	case 0:
		return "today"
	case -1:
		return "yesterday"
	case 1:
		return "tomorrow"
	}
	return ""
}

func ago(t, now time.Time) string {
	d := now.Sub(t)
	future := d < 0
	if future {
		d = -d
	}
	var n int
	var unit string
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		n, unit = int(d/time.Minute), "minute"
	case d < 24*time.Hour:
		n, unit = int(d/time.Hour), "hour"
	case d < 30*24*time.Hour:
		n, unit = int(d/(24*time.Hour)), "day"
	case d < 365*24*time.Hour:
		n, unit = int(d/(30*24*time.Hour)), "month"
	default:
		n, unit = int(d/(365*24*time.Hour)), "year"
	}
	s := strconv.Itoa(n) + " " + unit
	if n != 1 {
		s += "s"
	}
	if future {
		return "in " + s
	}
	return s + " ago"
}

func ordinal(d int) string {
	s := strconv.Itoa(d)
	switch {
	case d%100 >= 11 && d%100 <= 13:
		return s + "th"
	case d%10 == 1:
		return s + "st"
	case d%10 == 2:
		return s + "nd"
	case d%10 == 3:
		return s + "rd"
	}
	return s + "th"
}

// tsSeconds parses a Slack ts ("1700000000.000100", number or quoted).
func tsSeconds(raw []byte) int64 {
	if len(raw) == 0 {
		return 0
	}
	s := strings.Trim(string(raw), `"`)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
