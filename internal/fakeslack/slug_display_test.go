package fakeslack

import "testing"

// slugPad is prefix followed by "abcdefghij" repeated, n bytes in all.
func slugPad(prefix string, n int) string {
	b := []byte(prefix)
	for i := 0; len(b) < n; i++ {
		b = append(b, "abcdefghij"[i%10])
	}
	return string(b)
}

// TestSlugDisplayURL holds the fake's display_url to the server's: every
// pair is a live chat.slugifyUrl answer (enterprise host, 2026-11-14,
// synthetic example.com URLs, slug_type "link" for all of them).
func TestSlugDisplayURL(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://example.com/short", "example.com/short"},
		{slugPad("https://example.com/", 148), "example.com/abcdefghijab…"},
		{slugPad("https://example.com/", 149), "example.com/abcdefghijab…"},
		{slugPad("https://example.com/docs/guides/", 300), "example.com/docs/…/abcdefghijab…"},
		{slugPad("https://example.com/search?q=", 300), "example.com/search?q=abcdefghijabcdefghij…"},
		{slugPad("https://example.com/page#section-", 300), "example.com/page#…"},
		{slugPad("https://example.com/a/b/c/d?x=1&y=2#", 300), "example.com/a/b/…/d?x=1&y=2#…"},
		{slugPad("https://example.com/p?a=1?b=", 300), "example.com/p?a=1?b=abcdefghijabcdef…"},
		{slugPad("https://example.com/?q=", 300), "example.com/?q=abcdefghijabcdefghij…"},
		{slugPad("https://example.com/#", 300), "example.com/#…"},

		{"https://example.com", "example.com"},
		{"https://example.com/", "example.com"},
		{"https://www.example.com/a", "example.com/a"},
		{"https://example.com/a/b", "example.com/a/b"},
		{"https://example.com/a/b/c", "example.com/a/b/c"},
		{"https://example.com/a/b/c/d/e", "example.com/a/b/…/e"},
		{"https://example.com/abcdefghijklmnopqrstuvwxyz", "example.com/abcdefghijklmnopqrstuvwxyz"},
		{"https://example.com/abcdefghijkl", "example.com/abcdefghijkl"},
		{"https://example.com/abcdefghijklm", "example.com/abcdefghijklm"},
		{"https://example.com/one/abcdefghijklmnopqrstuvwxyz", "example.com/one/abcdefghijklmnopqrstuvwxyz"},
		{"https://example.com/p?a=1", "example.com/p?a=1"},
		{"https://example.com/p?abcdefghijklmnopqrstuvwxyz0123456789", "example.com/p?…"},
		{"https://example.com/p?q=abcdefghijklmnopqrst", "example.com/p?q=abcdefghijklmnopqrst"},
		{"https://example.com/p?q=abcdefghijklmnopqrstu", "example.com/p?q=abcdefghijklmnopqrst…"},
		{"https://example.com/p#a", "example.com/p#…"},
		{"https://example.com/p#abcdefghij", "example.com/p#…"},
		{"http://example.com/a", "example.com/a"},
		{"https://sub.domain.example.com/a", "sub.domain.example.com/a"},
		{"https://example.com:8080/a", "example.com/a"},
		{"https://user:pass@example.com/a", "example.com/a"},
		{"https://example.com/a/", "example.com/a"},
		{"https://example.com/a/b/", "example.com/a/b"},
		{"https://example.com/aa/bb/cc", "example.com/aa/bb/cc"},
		{"https://example.com/aaaaaaaaaa/bbbbbbbbbb/cccccccccc", "example.com/aaaaaaaaaa/bbbbbbbbbb/cccccccccc"},
		{"https://example.com/aaaaaaaaaaaaaaaaaaaa/b", "example.com/aaaaaaaaaaaaaaaaaaaa/b"},
		{"https://example.com/a/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "example.com/a/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{"https://example.com/a/b/c?x=1", "example.com/a/b/c?x=1"},
		{"https://example.com/a/b/c?x=1#f", "example.com/a/b/c?x=1#…"},
		{"https://example.com/a%20b/c", "example.com/a%20b/c"},
		{"https://example.com/%E6%97%A5%E6%9C%AC/x", "example.com/…/x"},

		{slugPad("https://example.com/", 51), slugPad("example.com/", 43)},
		{slugPad("https://example.com/", 56), slugPad("example.com/", 48)},
		{slugPad("https://example.com/", 60), "example.com/abcdefghijab…"},
		{slugPad("https://example.com/", 61), "example.com/abcdefghijab…"},
		{slugPad("https://example.com/", 70), "example.com/abcdefghijab…"},
		{slugPad("https://example.com/", 84), "example.com/abcdefghijab…"},
		{"https://example.com/a/b/c/d", "example.com/a/b/c/d"},
		{slugPad("https://example.com/docs/guides/", 72), "example.com/docs/guides/abcdefghijab…"},
		{slugPad("https://example.com/docs/", 66) + "/x", "example.com/docs/abcdefghijab…/x"},
		{"https://example.com/p?k=v&abcdefghijklmnopqrstuvwxyz=1", "example.com/p?k=v&…"},
		{slugPad("https://example.com/", 52), slugPad("example.com/", 44)},
		{slugPad("https://example.com/", 55), slugPad("example.com/", 47)},
		{slugPad("https://example.com/p?", 32), "example.com/p?…"},
		{slugPad("https://example.com/p?", 43), "example.com/p?…"},
		{"https://example.com/p?a=1&b=2&c=3&d=4", "example.com/p?a=1&b=2&…"},
		{"https://example.com/a/b/c/d/e?x=1", "example.com/a/b/…/e?x=1"},
		{"https://EXAMPLE.com/A", "EXAMPLE.com/A"},
	}
	for _, c := range cases {
		if got := slugDisplayURL(c.url); got != c.want {
			t.Errorf("%s\n got %s\nwant %s", c.url, got, c.want)
		}
	}
}
