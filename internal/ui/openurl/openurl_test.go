package openurl

import "testing"

func TestCheck(t *testing.T) {
	ok := map[string]string{
		"https://hackclub.slack.com/archives/C1/p1":  "https://hackclub.slack.com/archives/C1/p1",
		"http://example.com/a?b=c#d":                 "http://example.com/a?b=c#d",
		"HTTPS://Example.com/":                       "https://Example.com/",
		"https://example.com/%20space":               "https://example.com/%20space",
		"mailto:someone@example.com":                 "mailto:someone@example.com",
		"mailto:a@b.c?subject=hi&attach=/etc/passwd": "mailto:a@b.c?subject=hi",
		"mailto:a@b.c?Attachment=~/.ssh/id_rsa":      "mailto:a@b.c",
	}
	for in, want := range ok {
		got, err := Check(in)
		if err != nil || got != want {
			t.Errorf("Check(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "file:///etc/passwd", "FILE:///C:/Windows/System32/calc.exe", "javascript:alert(1)",
		"data:text/html,<script>", "slack://channel?id=C1", "vscode://file/x", "smb://host/share",
		"-a /bin/sh", "--help", "-https://x", "https://-oProxyCommand=x/", "https:///no-host", "https:opaque",
		"mailto:-x", "https://example.com/a b", "https://example.com/\x00", "https://example.com/\n",
		`https://example.com/"onload`, `\\server\share`, "/etc/passwd", "C:\\Windows\\calc.exe",
		"ms-msdt:/id", "search-ms:query=x", "x-apple.systempreferences:", "ftp://example.com/",
	} {
		if got, err := Check(in); err == nil {
			t.Errorf("Check(%q) allowed: %q", in, got)
		}
	}
}
