package core

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/store"
)

// ImageFile is AttachmentImage's Download (boot-shared UvHM, build
// 9f450b8ad3db): the Slack file an image's url is, as the client holds it,
// with its download URL. Slack reads the file's id from the url
// (getFileIdFromUrlWithState, dSnf), the file from its files store
// (getFileById: Clack's held copy, store.HeldFile) and its
// url_private_download (getFileDownloadUrl, I83v); without one the group
// draws no Download. ok false for an image that isn't a held Slack file.
func (a *App) ImageFile(u string) (*richtext.File, bool) {
	id := a.fileIDFromURL(u)
	if id == "" {
		return nil, false
	}
	f, ok := a.S.HeldFile(id)
	if !ok || f.URLPrivateDownload == "" {
		return nil, false
	}
	return richtext.FileOf(&f), true
}

// fileIDFromURL is getFileIdFromUrlWithState (dSnf): a url on a Slack
// host (isValidSlackHostName, dYCL) whose path, its slashes trimmed, is
//
//   - files/<member>/<id>/…: the member known (by id, else by name) and
//     not a stub;
//   - files-pri/<team>-<id>/…;
//   - files-tmb/<team>-<id>-<hash>/…;
//
// with id a file id (looksLikeFileId, oRye). "" for any other url.
//
// Slack reads new URL(url): the host lowercased, the pathname with its dot
// segments resolved (%2e ones too), %2F kept encoded and a backslash read
// as a slash. A path it reads otherwise than Go does (a "." or ".."
// segment, an escaped dot, slash or backslash, a backslash) names no file
// here: Slack's own file URLs carry none, and the image fetched would be
// another file than the one Download saves.
func (a *App) fileIDFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !slackHostName(strings.ToLower(u.Hostname())) {
		return ""
	}
	ep := strings.ToLower(u.EscapedPath())
	if strings.Contains(ep, "%2e") || strings.Contains(ep, "%2f") || strings.Contains(ep, "%5c") || strings.ContainsRune(u.Path, '\\') {
		return ""
	}
	p := strings.Trim(u.Path, "/")
	seg := strings.Split(p, "/")
	if slices.Contains(seg, ".") || slices.Contains(seg, "..") {
		return ""
	}
	switch {
	case strings.HasPrefix(p, "files/"):
		if len(seg) < 3 || seg[1] == "" || !a.urlMember(seg[1]) {
			return ""
		}
		if fileIDRe.MatchString(seg[2]) {
			return seg[2]
		}
	case strings.HasPrefix(p, "files-pri/"):
		if t := strings.Split(seg[1], "-"); len(seg) >= 3 && len(t) == 2 && fileIDRe.MatchString(t[1]) {
			return t[1]
		}
	case strings.HasPrefix(p, "files-tmb/"):
		if t := strings.Split(seg[1], "-"); len(seg) >= 3 && len(t) == 3 && fileIDRe.MatchString(t[1]) {
			return t[1]
		}
	}
	return ""
}

// urlMember is dSnf's member test: by id (looksLikeMemberId) the member
// the store holds, else a member named so (getMemberByName; Clack has
// names, not usernames alone, to match).
func (a *App) urlMember(m string) bool {
	if LooksLikeMemberID(m) {
		_, ok := a.S.User(strings.TrimPrefix(m, "@"))
		return ok
	}
	return len(a.S.UsersNamed([]string{m})[store.FoldName(m)]) > 0
}

// fileIDRe is looksLikeFileId (oRye).
var fileIDRe = regexp.MustCompile(`^F[A-Z0-9]{8,14}$`)

// slackHostRe is isValidSlackHostName's pattern (dYCL): a workspace or
// files host, staging and enterprise forms with it.
var slackHostRe = regexp.MustCompile(`^[^.]+\.(((enterprise\.)?(staging|staging-master|staging-sandbox|dev\d*|qa\d*)|enterprise)\.)?[^.]+\.[^.]+$`)

// slackApexDomains are /bpz's apex domains (isSlackUrl: the host's last
// two labels).
var slackApexDomains = []string{"slack.com", "slack-dev.com", "slack-gov.com", "slack-gov-dev.com", "slack.mil", "slack-mil-dev.com"}

// slackHostName is isValidSlackHostName: a host of a Slack apex domain in
// the pattern.
func slackHostName(h string) bool {
	if h == "" || !slackHostRe.MatchString(h) {
		return false
	}
	labels := strings.Split(h, ".")
	apex := strings.Join(labels[len(labels)-2:], ".")
	for _, d := range slackApexDomains {
		if apex == d {
			return true
		}
	}
	return false
}
