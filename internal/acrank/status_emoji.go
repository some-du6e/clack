package acrank

import (
	"strings"

	"github.com/go-text/typesetting/segmenter"
)

// The custom status modal's semantic emoji match (boot-shared D72a,
// CustomStatusModal; the semantic_emoji_matching experiment, O2By RY):
// while you type a status and haven't picked an emoji yourself, the emoji
// follows the text (debounced). The text is lowercased and split into
// words (Intl.Segmenter, granularity "word", the word-like segments); its
// runs of 4, 3, 2 and 1 words (plus "1:1" when the text has it) are
// looked up in the keyword table below; of the emoji found, the one
// earliest in the table wins, and no match clears the emoji. Slack sends
// each keyword (and emoji name) through its current_status I18n with the
// experiment on.

// statusEmojiTable is D72a's t3, in its order: emoji → keywords.
var statusEmojiTable = []struct {
	Name     string
	Keywords []string
}{
	{"dart", []string{"focus", "focusing", "focused", "concentrate", "concentrating", "deep work", "heads down", "quiet time"}},
	{"no_entry_sign", []string{"do not disturb", "busy", "unavailable", "dnd"}},
	{"face_with_thermometer", []string{"sick", "fever", "flu", "unwell", "covid", "ill", "cold"}},
	{"hospital", []string{"doctor", "hospital", "clinic", "medical", "checkup", "surgery"}},
	{"tooth", []string{"dentist", "teeth", "tooth"}},
	{"face_with_head_bandage", []string{"recovering", "injured", "injury", "hurt", "hurting"}},
	{"sneezing_face", []string{"allergy", "allergies", "allergic", "sneeze", "sneezing"}},
	{"no_entry", []string{"out of office", "away from work", "not working", "ooo"}},
	{"palm_tree", []string{"vacation", "on holiday", "paid time off", "on leave", "day off", "tropical", "pto"}},
	{"skier", []string{"ski trip", "skiing"}},
	{"airplane", []string{"travel", "traveling", "flight", "airport", "flying", "plane", "airplane"}},
	{"beach_with_umbrella", []string{"beach", "beaches", "coast", "coasts", "ocean", "oceans", "shore", "shores"}},
	{"camping", []string{"camping", "tent", "outdoors", "cabin"}},
	{"hiking_boot", []string{"hike", "trail", "trek", "trekking", "backpacking", "hiking"}},
	{"snow_capped_mountain", []string{"mountain", "climbing", "summit"}},
	{"baby_bottle", []string{"parental leave", "maternity", "paternity"}},
	{"scales", []string{"jury", "court"}},
	{"books", []string{"learn", "learning", "study", "studying", "studies", "research", "researcher", "researching", "lecture", "lecturing", "reading", "course", "workshop", "seminar", "certification", "tutorial", "homework", "exam"}},
	{"pizza", []string{"pizza", "calzone"}},
	{"sushi", []string{"sushi"}},
	{"taco", []string{"taco"}},
	{"hamburger", []string{"burger", "hamburger", "cheeseburger"}},
	{"ramen", []string{"ramen", "noodles", "soup", "pho"}},
	{"beer", []string{"beer", "pub", "happy hour", "drinks"}},
	{"wine_glass", []string{"wine"}},
	{"cocktail", []string{"cocktail"}},
	{"green_salad", []string{"lunch", "salad"}},
	{"coffee", []string{"coffee", "espresso", "latte", "cafe", "tea", "cappuccino"}},
	{"fried_egg", []string{"breakfast", "brunch"}},
	{"fork_and_knife", []string{"dinner", "food", "eating", "meal", "restaurant"}},
	{"double_vertical_bar", []string{"stepping away", "be right back", "away from keyboard", "break", "quick break", "brb", "afk"}},
	{"shopping_trolley", []string{"shopping", "grocery", "store", "market", "errand"}},
	{"telephone_receiver", []string{"call", "phone"}},
	{"headphones", []string{"huddle", "huddles", "huddling", "headphone", "listening", "zoom", "video", "webcam", "webinar", "google meet", "teams"}},
	{"date", []string{"meeting", "standup", "sync", "scrum", "retro", "demo", "calendar", "schedule", "kickoff", "all-hands", "all hands", "1:1", "townhall", "appointment", "interview", "one on one", "mtg"}},
	{"writing_hand", []string{"writing", "write", "draft", "blog", "document", "report", "proposal", "memo"}},
	{"eyes", []string{"code review", "code reviews", "pull request", "pull requests", "reviewing"}},
	{"computer", []string{"coding", "programming", "developing", "hacking", "building", "implement", "implementing", "implemented", "refactor", "refactoring"}},
	{"bug", []string{"debug", "debugging", "bug", "troubleshoot", "troubleshooting", "hotfix"}},
	{"rocket", []string{"deploy", "deployed", "deploying", "launch", "launched", "launching", "release", "released", "releasing", "publishing", "published"}},
	{"test_tube", []string{"test", "tests", "tested", "testing", "experiment", "experiments", "experimenting", "experimented"}},
	{"art", []string{"design", "designs", "designing", "designed", "sketch", "sketches", "sketching", "sketched", "figma", "mockup", "mockups", "wireframe", "wireframes", "prototype", "prototyping", "prototyped", "ui", "ux"}},
	{"microphone", []string{"presenting", "presentation", "speaking", "keynote", "keynotes", "pitch", "pitching", "talk", "talking"}},
	{"house", []string{"working from home", "work from home", "remote", "home office", "wfh"}},
	{"office", []string{"office", "onsite", "on-site", "coworking", "in office", "hq"}},
	{"bus", []string{"commute", "commuting", "transit", "train", "subway"}},
	{"muscle", []string{"gym", "workout", "working out", "exercise", "exercising", "lifting", "crossfit", "fitness"}},
	{"runner", []string{"run", "running", "ran", "jog", "jogging", "jogged", "marathon", "out for a walk"}},
	{"swimmer", []string{"swim", "swimming", "pool"}},
	{"person_in_lotus_position", []string{"yoga", "pilates", "stretching", "meditate", "meditating", "meditation", "mindfulness"}},
	{"relieved", []string{"relax", "relaxing", "relaxed", "chill", "chilling", "unwind", "unwinding", "decompress", "decompressing"}},
	{"sleeping", []string{"nap", "napping", "sleep", "sleeping", "tired", "exhausted", "resting"}},
	{"school", []string{"school", "daycare"}},
	{"baby", []string{"kid", "baby", "child", "toddler"}},
	{"family", []string{"family", "kids"}},
	{"dog", []string{"walking the dog", "dog", "puppy"}},
	{"cat", []string{"cat", "kitten", "feline"}},
	{"paw_prints", []string{"vet", "veterinary", "veterinarian", "grooming", "pet sitting"}},
	{"clapper", []string{"movie", "movies", "cinema", "cinemas", "film", "films", "theater", "theaters", "theatre", "theatres"}},
	{"studio_microphone", []string{"podcast", "audio", "recording", "broadcast"}},
	{"video_game", []string{"game", "gaming", "streaming"}},
	{"musical_note", []string{"concert", "music", "playlist", "festival", "gig"}},
	{"birthday", []string{"birthday", "anniversary"}},
	{"tada", []string{"celebrate", "celebrating", "celebrated", "party", "parties", "partied", "happy holidays"}},
	{"two_hearts", []string{"wedding", "married", "marriage", "honeymoon"}},
	{"jack_o_lantern", []string{"halloween"}},
	{"christmas_tree", []string{"christmas", "xmas"}},
	{"turkey", []string{"thanksgiving"}},
	{"clinking_glasses", []string{"new years", "new year’s", "new year", "nye", "cheers", "toast"}},
	{"thinking_face", []string{"brainstorm", "brainstorming", "ponder", "pondering", "reflect", "reflecting", "thinking"}},
	{"clipboard", []string{"plan", "planning", "planned", "strategy", "strategize", "roadmap", "priority", "prioritize", "sprint planning"}},
	{"star-struck", []string{"excited", "hyped", "stoked"}},
	{"snowflake", []string{"snow", "snowing", "snowed", "blizzard", "frost", "winter", "ice"}},
	{"thunder_cloud_and_rain", []string{"storm", "storming", "stormed", "thunder", "rain", "raining", "rained", "lightning"}},
	{"iphone", []string{"on mobile", "on phone", "mobile only"}},
	{"mute", []string{"notifications off", "muted", "silent"}},
	{"scissors", []string{"haircut", "barber", "salon"}},
}

// StatusEmoji is the emoji (a name, without colons) the custom status
// modal picks for status text, "" for none. tr translates a keyword or
// emoji name (Slack's current_status I18n; nil keeps the English).
func StatusEmoji(text string, tr func(string) string) string {
	if tr == nil {
		tr = func(s string) string { return s }
	}
	byKeyword := map[string]string{} // t6: a keyword's last emoji wins
	order := map[string]int{}        // t8: the emoji's place in the table
	for i, e := range statusEmojiTable {
		name := tr(e.Name)
		if _, ok := order[name]; !ok {
			order[name] = i
		}
		for _, k := range e.Keywords {
			byKeyword[tr(k)] = name
		}
	}
	best := ""
	for _, p := range statusPhrases(strings.ToLower(text)) {
		e, ok := byKeyword[p]
		if !ok {
			continue
		}
		if best == "" || order[e] < order[best] {
			best = e
		}
	}
	return best
}

// statusPhrases lists text's runs of 4, 3, 2 and 1 words, then "1:1" when
// text contains it.
func statusPhrases(text string) []string {
	words := statusWords(text)
	var out []string
	for n := 4; n >= 1; n-- {
		for i := 0; i+n <= len(words); i++ {
			out = append(out, strings.Join(words[i:i+n], " "))
		}
	}
	if strings.Contains(text, "1:1") {
		out = append(out, "1:1")
	}
	return out
}

// statusWords is Intl.Segmenter's word-like segments (UAX #29 word
// boundaries, words starting with a letter or a number).
func statusWords(text string) []string {
	var seg segmenter.Segmenter
	seg.Init([]rune(text))
	it := seg.WordIterator()
	var out []string
	for it.Next() {
		if w := it.Word(); len(w.Text) > 0 {
			out = append(out, string(w.Text))
		}
	}
	return out
}
