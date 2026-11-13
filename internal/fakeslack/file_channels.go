package fakeslack

// File channels, Size.FileChannels: the hidden conversations Slack keeps
// for a canvas's or list's comments, which users.conversations lists once
// the viewer is in them (core/sidebar_docs.go). Shaped like the real
// account's (2026-11-12, flags only): the file's id with F → C, is_file,
// is_channel, is_private, is_thread_only and is_org_shared, named after
// the file's title, spaces, capitals and :emoji: included. Two of them
// sit in the "Projects" section (as a viewer's pinned canvas and list
// do), the others in no section. Synthetic titles only.

import (
	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/slack"
)

const (
	// CanvasSnacks is a standalone canvas whose title starts with an emoji
	// shortcode; FileChanSnacks is its file channel, in Projects.
	CanvasSnacks   = "F0CANVSNAK1"
	FileChanSnacks = "C0CANVSNAK1"
	// CanvasStatus is a canvas with a plain title, its file channel in no
	// section.
	CanvasStatus   = "F0CANVSTAT1"
	FileChanStatus = "C0CANVSTAT1"
	// FileChanKitchen is ListKitchen's file channel, in Projects.
	FileChanKitchen = "C0LIST00001"
	// FileChanSection is the section the fixtures put file channels in.
	FileChanSection = "L0FAKE02"
	// CanvasSnacksTitle is CanvasSnacks's title.
	CanvasSnacksTitle = ":shopping_trolley: Snack run"
)

// fileChannelConv is a file channel as users.conversations lists it.
func fileChannelConv(id, title string) slack.Conversation {
	return slack.Conversation{ID: id, Name: title, IsChannel: true, IsPrivate: true, IsMember: true, IsFile: true,
		IsOrgShared: true, IsThreadOnly: bp(true), Created: 1762400000, Creator: "U0FAKE00001"}
}

// addFileChannels adds the file channels to the viewer's conversations.
func (s *Server) addFileChannels() {
	s.convs = append(s.convs,
		fileChannelConv(FileChanSnacks, CanvasSnacksTitle),
		fileChannelConv(FileChanStatus, "AI status board"),
		fileChannelConv(FileChanKitchen, "Launch tracker"))
}

// fileChannelCanvases are the canvases behind the file channels.
func fileChannelCanvases() []*FakeCanvas {
	return []*FakeCanvas{
		{ID: CanvasSnacks, Title: CanvasSnacksTitle, Doc: canvas.Doc{Blocks: canvas.ParseMarkdown("# Snack run\n\n- [ ] Crisps\n- [ ] Fruit", nil)}},
		{ID: CanvasStatus, Title: "AI status board", Doc: canvas.Doc{Blocks: canvas.ParseMarkdown("Everything is up.", nil)}},
	}
}

// fileChannelSectionIDs are the file channels a section lists.
func fileChannelSectionIDs() []string { return []string{FileChanSnacks, FileChanKitchen} }
