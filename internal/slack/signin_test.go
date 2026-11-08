package slack

import (
	"context"
	"strings"
	"testing"
)

func TestSignInValidatesBeforeNetwork(t *testing.T) {
	cases := []struct {
		in   SignInInput
		want string
	}{
		{SignInInput{Token: "xoxc-1", Cookie: "xoxd-1"}, "workspace"},
		{SignInInput{Workspace: "hackclub", Token: "nope", Cookie: "xoxd-1"}, "xoxc-"},
		{SignInInput{Workspace: "hackclub", Token: "xoxc-1", Cookie: "abc"}, "xoxd-"},
	}
	for _, c := range cases {
		_, err := SignIn(context.Background(), c.in)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("SignIn(%+v) = %v, want error mentioning %q", c.in, err, c.want)
		}
	}
}

func TestCleanCookie(t *testing.T) {
	cases := map[string]string{
		"xoxd-a/b+c=":         "xoxd-a%2Fb%2Bc%3D",
		"d=xoxd-abc":          "xoxd-abc",
		"xoxd-a%2Fb":          "xoxd-a%2Fb",
		"  xoxd-abc ; Path=/": "xoxd-abc",
		"":                    "",
	}
	for in, want := range cases {
		if got := cleanCookie(in, "d"); got != want {
			t.Errorf("cleanCookie(%q) = %q, want %q", in, got, want)
		}
	}
	if got := cleanCookie("d-s=1794000000", "d-s"); got != "1794000000" {
		t.Errorf("d-s = %q", got)
	}
}

func TestExpandPastedTeams(t *testing.T) {
	in := SignInInput{Workspace: "hackclub", Token: `[{"name":"Hack Club","id":"E1","url":"https://hackclub.enterprise.slack.com/","token":"xoxc-org"},{"name":"Hack Club","id":"T1","url":"https://hackclub.slack.com/","token":"xoxc-team"}]`, Cookie: "xoxd-1"}
	if err := expandPastedTeams(&in); err != nil {
		t.Fatal(err)
	}
	if in.Token != "xoxc-team" || in.OrgToken != "xoxc-org" || in.OrgURL != "https://hackclub.enterprise.slack.com/" || in.Workspace != "https://hackclub.slack.com/" {
		t.Fatalf("got %+v", in)
	}
	bare := SignInInput{Workspace: "hackclub", Token: "xoxc-abc"}
	if err := expandPastedTeams(&bare); err != nil || bare.Token != "xoxc-abc" {
		t.Fatalf("bare token changed: %+v %v", bare, err)
	}
	bad := SignInInput{Token: `[{"id":"E1","token":"xoxc-org"}]`}
	if err := expandPastedTeams(&bad); err == nil {
		t.Fatal("want error without a T… team")
	}
}
