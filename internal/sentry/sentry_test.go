package sentry

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/sentry/sentrytest"
)

func TestParseDSN(t *testing.T) {
	d, err := ParseDSN("https://abc123@o42.ingest.us.sentry.io/4504")
	if err != nil {
		t.Fatal(err)
	}
	if d.PublicKey != "abc123" || d.Host != "o42.ingest.us.sentry.io" || d.ProjectID != "4504" || d.Path != "" {
		t.Errorf("parsed %+v", d)
	}
	if got, want := d.EnvelopeURL(), "https://o42.ingest.us.sentry.io/api/4504/envelope/"; got != want {
		t.Errorf("envelope URL %q, want %q", got, want)
	}
	d, err = ParseDSN("https://k@sentry.example.com/prefix/7/")
	if err != nil || d.EnvelopeURL() != "https://sentry.example.com/prefix/api/7/envelope/" {
		t.Errorf("a DSN with a path: %v %q", err, d.EnvelopeURL())
	}
	if _, err := ParseDSN("http://k@127.0.0.1:9999/1"); err != nil {
		t.Errorf("plain http to loopback (the fake): %v", err)
	}
	for _, bad := range []string{
		"",
		"http://secretkey@sentry.example.com/1", // never plain http off loopback
		"https://sentry.example.com/1",          // no key
		"https://secretkey@sentry.example.com",  // no project
		"https://secretkey@sentry.example.com/notanumber",
		"ftp://secretkey@sentry.example.com/1",
		"https://secretkey@[::1/1",
	} {
		_, err := ParseDSN(bad)
		if err == nil {
			t.Errorf("ParseDSN(%q) took it", bad)
			continue
		}
		if strings.Contains(err.Error(), "secretkey") {
			t.Errorf("ParseDSN's error quotes the DSN: %v", err)
		}
	}
}

// A build without the DSN (every test binary, a local build) has no
// client: it can't send anything.
func TestNoDSNNoClient(t *testing.T) {
	if Configured() || Default("v1") != nil {
		t.Fatal("a test binary has a DSN: release.yml's -X must not reach tests")
	}
	var c *Client
	if _, err := c.Send(context.Background(), Feedback("hi", nil, false)); err == nil {
		t.Error("a nil client sent")
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A report is one envelope: a feedback item (Sentry's user feedback, its
// message the comment) with the report's rows as a context and tags,
// then the screenshot as an attachment.
func TestSendFeedbackEnvelope(t *testing.T) {
	fake := sentrytest.Start(t)
	c, err := New(fake.DSN(), "v0.1.0-alpha.169")
	if err != nil {
		t.Fatal(err)
	}
	shot := testPNG(t)
	rows := []ReportRow{{"component", "UnstyledButton"}, {"region", "primary"}, {"page", "c/ch:3fa2b1 (public)"}, {"empty", ""}}
	id, err := c.Send(context.Background(), Feedback("the pill is 1px low", rows, true), Attachment{Filename: "window.png", ContentType: "image/png", Data: shot})
	if err != nil {
		t.Fatal(err)
	}
	envs := fake.Wait(t, 1, 5*time.Second)
	if len(envs) != 1 {
		t.Fatalf("%d envelopes, want 1", len(envs))
	}
	e := envs[0]
	if e.Header["event_id"] != id || len(id) != 32 {
		t.Errorf("envelope event_id %v, Send returned %q", e.Header["event_id"], id)
	}
	if !strings.Contains(e.Auth, "sentry_client=clack/v0.1.0-alpha.169") {
		t.Errorf("auth %q", e.Auth)
	}
	if len(e.Items) != 2 || e.Items[0].Type() != "feedback" || e.Items[1].Type() != "attachment" {
		t.Fatalf("items %v", e.Items)
	}
	ev, err := e.Items[0].JSON()
	if err != nil {
		t.Fatal(err)
	}
	if ev["type"] != "feedback" || ev["level"] != "info" || ev["release"] != "clack@v0.1.0-alpha.169" || ev["event_id"] != id {
		t.Errorf("event %v", ev)
	}
	ctx, _ := ev["contexts"].(map[string]any)
	fb, _ := ctx["feedback"].(map[string]any)
	if fb["message"] != "the pill is 1px low" {
		t.Errorf("feedback context %v", fb)
	}
	rep, _ := ctx["report"].(map[string]any)
	if rep["component"] != "UnstyledButton" || rep["page"] != "c/ch:3fa2b1 (public)" || rep["empty"] != nil {
		t.Errorf("report context %v", rep)
	}
	tags, _ := ev["tags"].(map[string]any)
	if tags["component"] != "UnstyledButton" || tags["kind"] != "report" || tags["screenshot"] != "yes" || tags["goos"] == "" {
		t.Errorf("tags %v", tags)
	}
	for _, k := range []string{"user", "server_name", "request", "breadcrumbs", "extra", "modules"} {
		if _, ok := ev[k]; ok {
			t.Errorf("the event carries %q", k)
		}
	}
	att := e.Items[1]
	if att.Header["filename"] != "window.png" || att.Header["content_type"] != "image/png" || !bytes.Equal(att.Payload, shot) {
		t.Errorf("attachment %v, %d bytes", att.Header, len(att.Payload))
	}
}

// Sentry's 429 is ErrRateLimited; another failure says the status.
func TestSendFailures(t *testing.T) {
	fake := sentrytest.Start(t)
	c, err := New(fake.DSN(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	fake.Answer(http.StatusTooManyRequests)
	if _, err := c.Send(context.Background(), Feedback("x", nil, false)); !errors.Is(err, ErrRateLimited) {
		t.Errorf("429: %v", err)
	}
	fake.Answer(http.StatusInternalServerError)
	_, err = c.Send(context.Background(), Feedback("x", nil, false))
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("500: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), sentrytest.Key) {
		t.Errorf("an error quotes the key: %v", err)
	}
}

// release.yml embeds the DSN with -X <this package>.dsn on every build,
// from the SENTRY_DSN secret, for tag builds only (a dry run builds
// without it): a wrong path would make -X do nothing, silently.
func TestReleaseSetsTheDSN(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	pkg := reflect.TypeOf(Client{}).PkgPath()
	flag := "-X " + pkg + ".dsn=${SENTRY_DSN:-}"
	builds := regexp.MustCompile(`(?m)^.*go build .*-o build/.*$`).FindAllString(src, -1)
	flagsVar := regexp.MustCompile(`(?m)^\s*flags="[^"]*"`).FindString(src)
	if len(builds) < 3 {
		t.Fatalf("release.yml has %d go build lines, want linux, windows and macOS", len(builds))
	}
	for _, l := range builds {
		if !strings.Contains(l, flag) && !(strings.Contains(l, `-ldflags="$flags"`) && strings.Contains(flagsVar, flag)) {
			t.Errorf("a release build doesn't set the DSN (%s):\n%s", flag, strings.TrimSpace(l))
		}
	}
	if n := strings.Count(src, "secrets.SENTRY_DSN"); n == 0 {
		t.Error("release.yml never reads the SENTRY_DSN secret")
	}
	for _, l := range strings.Split(src, "\n") {
		if strings.Contains(l, "secrets.SENTRY_DSN") && !strings.Contains(l, "SENTRY_DSN: ") {
			t.Errorf("the secret is used outside an env: line, where a step's script could print it: %s", strings.TrimSpace(l))
		}
		if strings.Contains(l, "secrets.SENTRY_DSN") && !strings.Contains(l, "refs/tags/") {
			t.Errorf("the DSN goes into a build that isn't a tag's (a dry run): %s", strings.TrimSpace(l))
		}
	}
}
