package demo

// The bots' scripts: data/scenes*.json (ambient banter), data/replies*.json
// (answers to the viewer), data/polls*.json (Pulse) and data/agent*.json
// (Inkwell). All of it is written in character; nothing is generated.
//
// Text in every script may use these placeholders, expanded when posted:
//
//	{self}      a mention of the viewer (<@U0SELF0001>)
//	{you}       the viewer's first name ("Jordan")
//	{@handle}   a mention of a cast member (<@U0DEMOGORD> for {@gordon})
//	{#key}      a channel link (<#C0DEMOGENL|general> for {#general})
//	{here}      @here, {channel} @channel
//	{name}      in a reply: the first name of whoever the reply answers
//
// Channel keys: general, random, sales, party, it, warehouse, lunch,
// kudos, santa, code, group (the group DM), dm (the DM between the step's
// speaker and the viewer: gordon, priya, marisol, theo, kip and inkwell
// have one).

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed data/scenes*.json data/replies*.json data/polls*.json data/agent*.json
var scriptFS embed.FS

// Scene is one ambient bit: a few steps that play in order, a few
// seconds apart, across one or more channels.
type Scene struct {
	ID      string `json:"id"`
	Channel string `json:"channel"`          // the default channel key for its steps
	Weight  int    `json:"weight,omitempty"` // how often it's picked relative to others (0: 1)
	// Once scenes play at most once a session (announcements, plot
	// points); the rest can repeat after every other scene has had a turn.
	Once  bool        `json:"once,omitempty"`
	Steps []SceneStep `json:"steps"`
}

// SceneStep is one message or reaction.
type SceneStep struct {
	Who  string `json:"who"`            // a cast handle
	Text string `json:"text,omitempty"` // a message (empty with React)
	// Channel overrides the scene's channel key for this step.
	Channel string `json:"channel,omitempty"`
	// Thread posts the message as a reply in the thread of the scene's
	// first message in that channel; In picks another step's message
	// (its index, 0-based) as the thread parent instead.
	Thread bool `json:"thread,omitempty"`
	In     *int `json:"in,omitempty"`
	// Broadcast also sends a thread reply to the channel.
	Broadcast bool `json:"broadcast,omitempty"`
	// React adds an emoji reaction (a name without colons) to step To's
	// message (default: the previous message step).
	React string `json:"react,omitempty"`
	To    *int   `json:"to,omitempty"`
	// Pause is the seconds to wait before this step (default 4–14 s,
	// random); the typing indicator runs for part of it.
	Pause float64 `json:"pause,omitempty"`
}

// Replies are how the cast answers the viewer.
type Replies struct {
	// Intents are matched against the viewer's message, in order; a
	// message can match several. Patterns are lowercase words or phrases
	// matched on word boundaries; a pattern ending in * matches a prefix
	// ("laminat*"). The intent "question" also matches a message ending
	// in "?" and "fallback" matches anything.
	Intents []Intent `json:"intents"`
	// Owners are who answers a topic first when it comes up in a channel
	// (intent -> handles), e.g. honey -> dale, printer -> theo.
	Owners map[string][]string `json:"owners"`
	// ChannelOwners answer anything in their channel (channel key ->
	// handles), e.g. it -> theo.
	ChannelOwners map[string][]string `json:"channel_owners"`
	// Lines are each person's answers by intent (handle -> intent ->
	// lines). Every person has a "fallback" and a "dm_fallback".
	Lines map[string]map[string][]string `json:"lines"`
	// Reactions are the emoji the cast reacts with by intent (intent ->
	// names without colons; custom names that the workspace lacks are
	// skipped). "fallback" is used when nothing matched.
	Reactions map[string][]string `json:"reactions"`
	// Huddle lines by caller handle: "missed" after an unanswered ring,
	// "declined" after the viewer declines, "no_audio" when the viewer
	// tries to join (the demo has no calls).
	Huddle map[string]map[string][]string `json:"huddle"`
}

// Intent is a topic or speech act the viewer's message can match.
type Intent struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"`
}

// Poll is one of Pulse's polls.
type Poll struct {
	ID       string       `json:"id"`
	Channel  string       `json:"channel"` // channel key (lunch)
	Question string       `json:"question"`
	Options  []PollOption `json:"options"`
	// Votes are the cast's scripted votes (handle -> option value); they
	// trickle in after the poll is posted.
	Votes map[string]string `json:"votes"`
	// Comments are thread replies posted while the votes come in (the
	// speaker votes first).
	Comments []PollComment `json:"comments,omitempty"`
}

// PollOption is a poll button.
type PollOption struct {
	Value string `json:"value"` // stable id: "tacos"
	Emoji string `json:"emoji"` // ":taco:"
	Text  string `json:"text"`  // "Taco Tuesday at Casa Verde"
}

// PollComment is a thread reply under a poll.
type PollComment struct {
	Who  string `json:"who"`
	Text string `json:"text"`
}

// Standup is Pulse's stand-up check-in (a static_select of moods).
type Standup struct {
	Prompt  string       `json:"prompt"`
	Options []PollOption `json:"options"`
	// Answers are the cast's scripted picks (handle -> option value).
	Answers map[string]string `json:"answers"`
	// Thanks is what Pulse says in the thread when the viewer picks
	// (option value -> line), "" key for any.
	Thanks map[string]string `json:"thanks"`
}

// AgentScript is Inkwell's repertoire.
type AgentScript struct {
	// Answers are matched on the prompt like Intents (first match wins;
	// one with no patterns is the fallback).
	Answers []AgentAnswer `json:"answers"`
	// Progress are the code channel updates Inkwell posts every few
	// minutes, in order (then it loops over the last few).
	Progress []AgentProgress `json:"progress"`
	// Prompts are the suggested prompts shown after a reply.
	Prompts []AgentPrompt `json:"prompts"`
}

// AgentAnswer is one streamed reply: the plan's tasks (each with an
// optional detail line), then the text, streamed in chunks.
type AgentAnswer struct {
	Patterns []string    `json:"patterns"`
	Tasks    []AgentTask `json:"tasks"`
	Text     string      `json:"text"`
}

// AgentTask is a plan task.
type AgentTask struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Output string `json:"output,omitempty"`
}

// AgentProgress is one code channel update: a task card, then (with CI)
// the channel's ci_state going pending then CI's result.
type AgentProgress struct {
	Card   string `json:"card"`             // task card title ("Pushed 2 commits")
	Detail string `json:"detail,omitempty"` // its details line
	Output string `json:"output,omitempty"` // shown when complete
	Text   string `json:"text,omitempty"`   // an optional message after it
	CI     string `json:"ci,omitempty"`     // "success" or "failure": runs CI
	// Reply is an optional cast reaction in the thread (who: text).
	Who   string `json:"who,omitempty"`
	Reply string `json:"reply,omitempty"`
}

// AgentPrompt is a suggested prompt.
type AgentPrompt struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

// Scripts is everything in data/ the bots read.
type Scripts struct {
	Scenes  []Scene
	Replies Replies
	Polls   []Poll
	Standup Standup
	Agent   AgentScript
}

// loadScripts reads and merges data/scenes*.json, replies*.json,
// polls*.json and agent*.json.
func loadScripts() (*Scripts, error) {
	s := &Scripts{Replies: Replies{Owners: map[string][]string{}, ChannelOwners: map[string][]string{},
		Lines: map[string]map[string][]string{}, Reactions: map[string][]string{}, Huddle: map[string]map[string][]string{}}}
	ents, err := fs.ReadDir(scriptFS, "data")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := scriptFS.ReadFile(path.Join("data", n))
		if err != nil {
			return nil, err
		}
		dec := func(v any) error {
			if err := json.Unmarshal(b, v); err != nil {
				return fmt.Errorf("demo: data/%s: %w", n, err)
			}
			return nil
		}
		switch {
		case strings.HasPrefix(n, "scenes"):
			var f struct {
				Scenes []Scene `json:"scenes"`
			}
			if err := dec(&f); err != nil {
				return nil, err
			}
			s.Scenes = append(s.Scenes, f.Scenes...)
		case strings.HasPrefix(n, "replies"):
			var f Replies
			if err := dec(&f); err != nil {
				return nil, err
			}
			s.Replies.Intents = append(s.Replies.Intents, f.Intents...)
			mergeLists(s.Replies.Owners, f.Owners)
			mergeLists(s.Replies.ChannelOwners, f.ChannelOwners)
			mergeLists(s.Replies.Reactions, f.Reactions)
			for h, m := range f.Lines {
				if s.Replies.Lines[h] == nil {
					s.Replies.Lines[h] = map[string][]string{}
				}
				mergeLists(s.Replies.Lines[h], m)
			}
			for h, m := range f.Huddle {
				if s.Replies.Huddle[h] == nil {
					s.Replies.Huddle[h] = map[string][]string{}
				}
				mergeLists(s.Replies.Huddle[h], m)
			}
		case strings.HasPrefix(n, "polls"):
			var f struct {
				Polls   []Poll   `json:"polls"`
				Standup *Standup `json:"standup"`
			}
			if err := dec(&f); err != nil {
				return nil, err
			}
			s.Polls = append(s.Polls, f.Polls...)
			if f.Standup != nil {
				s.Standup = *f.Standup
			}
		case strings.HasPrefix(n, "agent"):
			var f AgentScript
			if err := dec(&f); err != nil {
				return nil, err
			}
			s.Agent.Answers = append(s.Agent.Answers, f.Answers...)
			s.Agent.Progress = append(s.Agent.Progress, f.Progress...)
			s.Agent.Prompts = append(s.Agent.Prompts, f.Prompts...)
		}
	}
	return s, nil
}

func mergeLists(dst, src map[string][]string) {
	for k, v := range src {
		dst[k] = append(dst[k], v...)
	}
}
