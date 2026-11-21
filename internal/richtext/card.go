package richtext

// The card block, as Slack's web client draws it (build 031dbe6a7400):
// the card renderable (boot-shared ARtW) is Card (7Xkb), a Slack Kit
// entity card (aJif, expanded, full width and height, not interactive):
// its hero image, a header (the slack_icon glyph or the icon image, then
// StackedLabels' title and subtitle), the body, the subtext and the
// actions, split by partitionActions (fWi9) into a left and a right
// group.

import (
	"encoding/json"
	"sort"
)

// Card is what a card block shows.
type Card struct {
	// Hero is hero_image's url and HeroAlt its alt text ("" none).
	Hero, HeroAlt string
	// Icon is the icon image's url; SlackIcon the slack_icon's name, a
	// 24px glyph in the icon's 36px box, which wins over Icon.
	Icon, SlackIcon string
	// Title and Subtitle are StackedLabels' two lines (one line each),
	// Subtext the caption under the body.
	Title, Subtitle, Subtext []Span
	Body                     []Block
	// Left and Right are the actions' two groups (partitionActions).
	Left, Right []Button
}

// bkCard is a card block's own fields (bk embeds it).
type bkCard struct {
	HeroImage *bk `json:"hero_image"`
	SlackIcon *struct {
		Name string `json:"name"`
	} `json:"slack_icon"`
	Body    *textObj `json:"body"`
	Subtext *textObj `json:"subtext"`
	Actions []bk     `json:"actions"`
	StyleV2 string   `json:"style_v2"` // a card action's (getButtonType)
}

// cardBlock converts a card block.
func (c *builder) cardBlock(b *bk) Block {
	cd := &Card{}
	if h := b.HeroImage; h != nil {
		if img := c.imageOf(h); img != nil {
			cd.Hero, cd.HeroAlt = img.URL, h.AltText
		}
	}
	if len(b.Icon) > 0 && b.Icon[0] == '{' {
		var ic bk
		if json.Unmarshal(b.Icon, &ic) == nil {
			if img := c.imageOf(&ic); img != nil {
				cd.Icon = img.URL
			}
		}
	}
	if b.SlackIcon != nil {
		cd.SlackIcon = b.SlackIcon.Name
	}
	if b.Title != nil {
		cd.Title = c.textSpans(b.Title)
	}
	if b.Subtitle != nil {
		cd.Subtitle = c.textSpans(b.Subtitle)
	}
	if b.Body != nil {
		cd.Body = c.textBlocks(b.Body)
	}
	if b.Subtext != nil {
		cd.Subtext = c.textSpans(b.Subtext)
	}
	var btns []Button
	for i := range b.Actions {
		a := &b.Actions[i]
		if btn, ok := c.element(a, b.BlockID); ok {
			btn.Style = buttonType(a)
			btns = append(btns, btn)
		}
	}
	cd.Left, cd.Right = partitionActions(btns)
	return Block{Kind: CardBlock, BlockID: b.BlockID, Card: cd}
}

// buttonType is getButtonType (szY5) as a Button's Style: style_v2
// (primary, danger; default and ghost draw as the outline button) or
// style, else the outline button ("").
func buttonType(e *bk) string {
	switch e.StyleV2 {
	case "primary", "danger":
		return e.StyleV2
	case "default", "ghost":
		return ""
	}
	if s := e.styleString(); s == "primary" || s == "danger" {
		return s
	}
	return ""
}

// partitionActions is fWi9's: with three actions or more the danger ones
// go left; the rest sit right, danger first and primary last.
func partitionActions(bs []Button) (left, right []Button) {
	rank := func(b Button) int {
		switch b.Style {
		case "danger":
			return 0
		case "primary":
			return 2
		}
		return 1
	}
	split := len(bs) >= 3
	for _, b := range bs {
		if split && b.Style == "danger" {
			left = append(left, b)
			continue
		}
		right = append(right, b)
	}
	sort.SliceStable(right, func(i, j int) bool { return rank(right[i]) < rank(right[j]) })
	return left, right
}
