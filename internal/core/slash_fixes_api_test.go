package core

// The fixes' API the tests call: one place for the red tree to stand in
// for (red-evidence-branch).

// topicOpts is /topic's options with the composer's cleaned topic.
func topicOpts(ch, text, submit string) CommandOpts {
	return CommandOpts{Channel: ch, Command: "/topic", Disp: "/topic", Text: text, Submit: submit, Params: []CommandParam{}}
}
