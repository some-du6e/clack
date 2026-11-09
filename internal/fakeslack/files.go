package fakeslack

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// File fixtures: Size.Files adds #files-gallery, one message per way
// Slack draws a shared file (FileGallery in channels: gallery v3 for one
// to six images, a portrait, a video, an image with a video; the audio
// player and a voice clip; a PDF, a snippet, a zip; two documents in the
// mixed grid) plus an image the viewer uploaded (its file menu has Delete
// file), a wide short screenshot and a small one (thumbs only up to
// thumb_360, like a pasted 1022x125 composer or a 223px crop), and a thread
// whose replies are a single image and a "+ N" gallery. files.info answers
// for every file here. Every image is this server's /img gradient;
// nothing is a real file. Messages go oldest first: FilesCases order.

const (
	FilesChannelID = "C0FILES0001"
	filesBaseTS    = 1762600000
)

// FilesCase is one #files-gallery message.
type FilesCase struct {
	Name string // what it shows
	TS   string
}

type filesFixture struct {
	name, text string
	user       int // index into s.users; -1 is the viewer
	files      []slack.File
	replies    []filesFixture // a thread under it
}

// FilesThread is the #files-gallery thread's root (FilesCases "thread").
const FilesThread = "thread"

// FilesCases are the #files-gallery messages in channel order.
func (s *Server) FilesCases() []FilesCase {
	var out []FilesCase
	for i, f := range s.filesFixtures() {
		out = append(out, FilesCase{Name: f.name, TS: filesTS(i)})
	}
	return out
}

func filesTS(i int) string { return strconv.Itoa(filesBaseTS+i*60) + ".000100" }

// fakeImage is an image file object with Slack's thumbs drawn by /img:
// thumb_64, _80, _160 and _360 always (thumb_360 at most the original's
// size), the larger ones (480, 720, 1024, with their sizes) only for an
// original bigger than them.
func (s *Server) fakeImage(id, name string, w, h int) slack.File {
	img := func(px, py int) string {
		return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(py) + "/" + id + ".png"
	}
	fit := func(side int) (int, int) {
		if max(w, h) <= side {
			return w, h
		}
		if w >= h {
			return side, max(1, side*h/w)
		}
		return max(1, side*w/h), side
	}
	f := slack.File{ID: id, Name: name, Title: name, Mimetype: "image/png", Filetype: "png", PrettyType: "PNG",
		Size: int64(w * h / 3), Mode: "hosted", URLPrivate: img(w, h), URLPrivateDownload: img(w, h),
		Permalink: s.URL + "files/" + id + "/" + name, OriginalW: w, OriginalH: h, Created: filesBaseTS}
	f.Thumb64 = img(fit(64))
	f.Thumb80 = img(fit(80))
	f.Thumb160 = img(fit(160))
	f.Thumb360W, f.Thumb360H = fit(360)
	f.Thumb360 = img(f.Thumb360W, f.Thumb360H)
	if max(w, h) > 480 {
		f.Thumb480W, f.Thumb480H = fit(480)
		f.Thumb480 = img(f.Thumb480W, f.Thumb480H)
	}
	if max(w, h) > 720 {
		f.Thumb720W, f.Thumb720H = fit(720)
		f.Thumb720 = img(f.Thumb720W, f.Thumb720H)
	}
	if max(w, h) > 720 {
		f.Thumb1024W, f.Thumb1024H = fit(min(1024, max(w, h)))
		f.Thumb1024 = img(f.Thumb1024W, f.Thumb1024H)
	}
	return f
}

func (s *Server) filesFixtures() []filesFixture {
	im := func(n, w, h int) slack.File {
		return s.fakeImage(fmt.Sprintf("F0FILESIMG%02d", n), fmt.Sprintf("photo-%d.png", n), w, h)
	}
	land, port, sq := [2]int{1600, 900}, [2]int{900, 1600}, [2]int{1000, 1000}
	ims := func(base int, sizes ...[2]int) []slack.File {
		var out []slack.File
		for i, sz := range sizes {
			out = append(out, im(base+i, sz[0], sz[1]))
		}
		return out
	}
	video := slack.File{ID: "F0FILESVID01", Name: "demo-walkthrough.mp4", Title: "demo-walkthrough.mp4", Mimetype: "video/mp4",
		Filetype: "mp4", PrettyType: "MPEG 4 Video", Size: 4 << 20, Mode: "hosted",
		URLPrivate: s.URL + "files-pri/" + TeamID + "-F0FILESVID01/demo-walkthrough.mp4",
		MP4:        s.URL + "files-pri/" + TeamID + "-F0FILESVID01/demo-walkthrough.mp4",
		ThumbVideo: s.URL + "img/1280x720/F0FILESVID01.png", ThumbVideoW: 1280, ThumbVideoH: 720,
		OriginalW: 1280, OriginalH: 720, DurationMS: 83000, Permalink: s.URL + "files/F0FILESVID01/demo-walkthrough.mp4"}
	audio := slack.File{ID: "F0FILESAUD01", Name: "standup-notes.mp3", Title: "standup-notes.mp3", Mimetype: "audio/mpeg",
		Filetype: "mp3", PrettyType: "MP3", Size: 900 << 10, Mode: "hosted", DurationMS: 154000,
		URLPrivate: s.URL + "files-pri/" + TeamID + "-F0FILESAUD01/standup-notes.mp3", Permalink: s.URL + "files/F0FILESAUD01/standup-notes.mp3"}
	clip := slack.File{ID: "F0FILESAUD02", Name: "audio_message.webm", Title: "Audio clip", Mimetype: "audio/webm",
		Filetype: "webm", PrettyType: "WebM", Subtype: "slack_audio", Size: 60 << 10, Mode: "hosted", DurationMS: 12000,
		AudioWaveSamples: []int{4, 9, 18, 30, 44, 61, 72, 80, 66, 52, 47, 58, 70, 84, 95, 88, 71, 55, 40, 33, 41, 57, 69, 62, 48, 35, 22, 14, 9, 5},
		URLPrivate:       s.URL + "files-pri/" + TeamID + "-F0FILESAUD02/audio_message.webm", Permalink: s.URL + "files/F0FILESAUD02/audio_message.webm"}
	pdf := slack.File{ID: "F0FILESPDF01", Name: "q3-report.pdf", Title: "Q3 report", Mimetype: "application/pdf", Filetype: "pdf",
		PrettyType: "PDF", Size: 312 << 10, Mode: "hosted", ThumbPDF: s.URL + "img/612x792/F0FILESPDF01.png", ThumbPDFW: 612, ThumbPDFH: 792,
		URLPrivate: s.URL + "files-pri/" + TeamID + "-F0FILESPDF01/q3-report.pdf", Permalink: s.URL + "files/F0FILESPDF01/q3-report.pdf"}
	snippet := slack.File{ID: "F0FILESSNP01", Name: "fib.py", Title: "fib.py", Mimetype: "text/plain", Filetype: "python",
		PrettyType: "Python", Size: 160, Mode: "snippet", Lines: 7,
		Preview:    "def fib(n):\n    a, b = 0, 1\n    for _ in range(n):\n        a, b = b, a + b\n    return a\n\nprint(fib(10))",
		URLPrivate: s.URL + "files-pri/" + TeamID + "-F0FILESSNP01/fib.py", Permalink: s.URL + "files/F0FILESSNP01/fib.py"}
	zip := slack.File{ID: "F0FILESZIP01", Name: "assets.zip", Title: "assets.zip", Mimetype: "application/zip", Filetype: "zip",
		PrettyType: "Zip", Size: 7 << 20, Mode: "hosted",
		URLPrivate: s.URL + "files-pri/" + TeamID + "-F0FILESZIP01/assets.zip", Permalink: s.URL + "files/F0FILESZIP01/assets.zip"}
	doc := slack.File{ID: "F0FILESDOC01", Name: "onboarding.docx", Title: "onboarding.docx", Mimetype: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Filetype: "docx", PrettyType: "Word Document", Size: 48 << 10, Mode: "hosted",
		URLPrivate: s.URL + "files-pri/" + TeamID + "-F0FILESDOC01/onboarding.docx", Permalink: s.URL + "files/F0FILESDOC01/onboarding.docx"}
	own := im(40, 1200, 800)
	own.Name, own.Title, own.User = "my-screenshot.png", "my-screenshot.png", SelfID
	mixed := im(30, 1600, 900)
	// pasted screenshots: Slack names them image.png and, for these, made
	// no thumb past thumb_360 (the wide one's is 360x44)
	wide := s.fakeImage("F0FILESIMG41", "image.png", 1022, 125)
	wide.Thumb480, wide.Thumb480W, wide.Thumb480H = "", 0, 0
	wide.Thumb720, wide.Thumb720W, wide.Thumb720H = "", 0, 0
	wide.Thumb1024, wide.Thumb1024W, wide.Thumb1024H = "", 0, 0
	wide.MediaDisplayType, wide.FileAccess = "unknown", "visible"
	tiny := s.fakeImage("F0FILESIMG42", "image.png", 223, 150)
	tiny.MediaDisplayType, tiny.FileAccess = "unknown", "visible"
	return []filesFixture{
		{"one image", "One landscape image", 1, ims(1, land), nil},
		{"portrait image", "A portrait", 2, ims(2, port), nil},
		{"two images", "Two side by side", 3, ims(3, land, sq), nil},
		{"three images", "Three in a row", 1, ims(5, land, port, sq), nil},
		{"four images", "Four in a row", 2, ims(8, land, land, sq, port), nil},
		{"six images", "Six: the fourth tile reads + 3", 3, ims(12, land, land, land, land, sq, port), nil},
		{"video", "A video", 1, []slack.File{video}, nil},
		{"image and video", "An image and a video", 2, []slack.File{mixed, video}, nil},
		{"audio", "An audio file", 3, []slack.File{audio}, nil},
		{"voice clip", "", 1, []slack.File{clip}, nil},
		{"pdf", "The PDF", 2, []slack.File{pdf}, nil},
		{"snippet", "A snippet", 3, []slack.File{snippet}, nil},
		{"zip", "A zip", 1, []slack.File{zip}, nil},
		{"two documents", "Two documents", 2, []slack.File{pdf, doc}, nil},
		{"own image", "My own upload", -1, []slack.File{own}, nil},
		{"wide short image", "this too", 1, []slack.File{wide}, nil},
		{"tiny image", "a small paste", 1, []slack.File{tiny}, nil},
		{FilesThread, "A thread of images", 2, nil, []filesFixture{
			{"thread image", "One in the thread", 1, ims(50, land), nil},
			{"thread gallery", "Six in the thread", 3, ims(51, land, land, land, land, sq, port), nil},
		}},
	}
}

// FilesHistory is #files-gallery's history, oldest first (thread roots
// carry their reply counts), for tests that seed a store with it.
func (s *Server) FilesHistory() []slack.Message {
	var out []slack.Message
	for i, f := range s.filesFixtures() {
		out = append(out, s.filesMessage(f, filesTS(i), ""))
	}
	return out
}

// FilesReplies is the thread under the #files-gallery message ts, root
// first (nil when it has none).
func (s *Server) FilesReplies(ts string) []slack.Message {
	for i, f := range s.filesFixtures() {
		if filesTS(i) != ts || len(f.replies) == 0 {
			continue
		}
		out := []slack.Message{s.filesMessage(f, ts, "")}
		for j, r := range f.replies {
			out = append(out, s.filesMessage(r, filesReplyTS(i, j), ts))
		}
		return out
	}
	return nil
}

// FilesReplyTS is the ts of the j-th reply under #files-gallery case name.
func (s *Server) FilesReplyTS(name string, j int) string {
	for i, f := range s.filesFixtures() {
		if f.name == name {
			return filesReplyTS(i, j)
		}
	}
	return ""
}

func filesReplyTS(i, j int) string { return strconv.Itoa(filesBaseTS+i*60+10*(j+1)) + ".000100" }

func (s *Server) filesMessage(f filesFixture, ts, thread string) slack.Message {
	user := SelfID
	if f.user >= 0 {
		user = s.users[f.user%len(s.users)].ID
	}
	files := append([]slack.File(nil), f.files...)
	for j := range files {
		if files[j].User == "" {
			files[j].User = user
		}
	}
	m := slack.Message{Type: "message", TS: ts, User: user, Text: f.text, Files: files,
		Upload: len(files) > 0, ClientMsgID: "files-" + ts}
	if thread != "" {
		m.ThreadTS = thread
	}
	if n := len(f.replies); n > 0 {
		m.ThreadTS, m.ReplyCount = ts, n
		i := 0
		for k, x := range s.filesFixtures() {
			if x.name == f.name {
				i = k
			}
		}
		m.LatestReply = filesReplyTS(i, n-1)
		for _, r := range f.replies {
			m.ReplyUsers = append(m.ReplyUsers, s.users[r.user%len(s.users)].ID)
		}
	}
	return m
}

// handleFiles answers files.info for the #files-gallery files and
// conversations.replies for its thread; handled=false passes on.
func (s *Server) handleFiles(method string, f url.Values) (any, bool) {
	if !s.Size.Files {
		return nil, false
	}
	switch method {
	case "files.info":
		id := f.Get("file")
		var walk func(fx []filesFixture) (slack.File, bool)
		walk = func(fx []filesFixture) (slack.File, bool) {
			for _, c := range fx {
				for _, file := range c.files {
					if file.ID == id {
						return file, true
					}
				}
				if file, found := walk(c.replies); found {
					return file, true
				}
			}
			return slack.File{}, false
		}
		if file, found := walk(s.filesFixtures()); found {
			return ok("file", file, "comments", []any{}), true
		}
	case "conversations.replies":
		if f.Get("channel") == FilesChannelID {
			if msgs := s.FilesReplies(f.Get("ts")); msgs != nil {
				return ok("messages", msgs, "has_more", false), true
			}
		}
	}
	return nil, false
}

func (s *Server) addFilesChannel() {
	s.convs = append(s.convs, slack.Conversation{ID: FilesChannelID, Name: "files-gallery", IsChannel: true, IsMember: true,
		NumMembers: 6, ContextTeamID: TeamID, Purpose: &slack.Purpose{Value: "File fixtures."}})
	msgs := s.FilesHistory()
	// history is newest first
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	s.hist[FilesChannelID] = msgs
}
