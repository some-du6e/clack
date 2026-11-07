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
		if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, filepath.Join("slack", "client.go")) {
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
