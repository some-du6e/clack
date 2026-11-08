package slack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Clack must never end the user's Slack session.
func TestNoSessionEndingMethods(t *testing.T) {
	c := New(Creds{Token: "t", TeamURL: "http://127.0.0.1:1/"})
	for m := range forbiddenMethods {
		if err := c.Call(context.Background(), m, nil, nil); !errors.Is(err, ErrForbiddenMethod) {
			t.Errorf("Call(%s) = %v, want ErrForbiddenMethod", m, err)
		}
	}
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, filepath.Join("slack", "client.go")) ||
			// the plugin fetch deny list (signOutMethods) names them only to refuse them
			strings.HasSuffix(p, filepath.Join("jsrt", "boot.go")) {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range []string{"auth.signout", "auth.revoke", "enterpriseSignout"} {
			if strings.Contains(string(b), m) {
				t.Errorf("%s mentions %s", p, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoginDoesNotFetch(t *testing.T) {
	if _, err := Login(context.Background(), "hackclub", "xoxd-synthetic"); !errors.Is(err, ErrLoginUnavailable) {
		t.Errorf("Login = %v", err)
	}
}

// Method names that aren't plain names are refused, so a plugin can't reach
// /api/auth.signout around the exact-name check.
func TestRefusedMethodSpellings(t *testing.T) {
	for _, m := range []string{"auth.signout?x=1", "auth.signout/", "../auth.signout", "AUTH.SIGNOUT", "Auth.EnterpriseSignout", "auth.revoke#", "auth.signout ", ""} {
		if !refusedMethod(m) {
			t.Errorf("refusedMethod(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"auth.test", "conversations.history", "users.prefs.get", "client.counts"} {
		if refusedMethod(m) {
			t.Errorf("refusedMethod(%q) = true, want false", m)
		}
	}
}

// d-s is never sent: Slack re-sets it to the current time on every response,
// so a stored one is always stale (docs/SIGNOUT-AUDIT.md).
func TestCookieHeaderNeverSendsDS(t *testing.T) {
	c := New(Creds{Token: "xoxc-1", Cookie: "xoxd-1", CookieDS: "1794000000"})
	if h := c.cookieHeader(); h != "d=xoxd-1" {
		t.Errorf("cookieHeader = %q, want d only", h)
	}
}
