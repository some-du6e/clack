package composer

// Delta returns the document as a Quill delta, {ops: [...]}, the shape
// Slack's composer hands to prepareAndSendMessage and stores as a draft's
// ops. Text runs carry bold/italic/underline/strike/code/link attributes;
// each line ends with a "\n" insert carrying the line format (list,
// indent, blockquote, code-block, header); mentions, channels, broadcasts
// and emoji are embeds. Only built for plugins (a patched composer).
//
// The embed names (slackuser, slackchannel, slackusergroup, slackemoji)
// are Clack's: plugins that need exact blocks get them from the "blocks"
// Clack passes next to the delta. Broadcasts (@channel, @here, @everyone)
// are Slack's own mention ops, {insert: "@here", attributes:
// {slackmention: {id: "BKhere", label: "@here"}}} (Slack's B7wc
// isOpMention / getMentionOpIdText), because plugins read their ids.
func (d *Doc) Delta() map[string]any {
	ops, _ := d.deltaOps(false)
	return map[string]any{"ops": ops}
}

// deltaOps is Delta's ops and, with spans, each op's runes in d: a text
// run's [start, end), an embed's one rune, a line's "\n" its newline (the
// closing one, which d has no rune for, [len, len)); a divider's embed has
// none (its line's text isn't content).
func (d *Doc) deltaOps(spans bool) ([]any, [][2]int) {
	ops := make([]any, 0, 8)
	var sp [][2]int
	add := func(op any, start, end int) {
		ops = append(ops, op)
		if spans {
			sp = append(sp, [2]int{start, end})
		}
	}
	line := 0
	lineOp := func(nl int) {
		var l Line
		if line < len(d.Lines) {
			l = d.Lines[line]
		}
		line++
		if l.Kind == Divider {
			add(map[string]any{"insert": map[string]any{"divider": true}}, nl, nl)
		}
		at := map[string]any{}
		switch l.Kind {
		case Bullet:
			at["list"] = "bullet"
		case Ordered:
			at["list"] = "ordered"
		case Quote:
			at["blockquote"] = true
		case CodeBlock:
			at["code-block"] = true
		case Header1:
			at["header"] = 1
		case Header2:
			at["header"] = 2
		case Header3:
			at["header"] = 3
		}
		if l.Indent > 0 && (l.Kind == Bullet || l.Kind == Ordered) {
			at["indent"] = l.Indent
		}
		if l.Quoted && (l.Kind == Bullet || l.Kind == Ordered || l.Kind == CodeBlock) {
			at["border"] = "1" // list autofill's and setCodeBlockOnLine's value on a quote line
		}
		op := map[string]any{"insert": "\n"}
		if len(at) > 0 {
			op["attributes"] = at
		}
		add(op, nl, min(nl+1, len(d.Runes)))
	}
	for i := 0; i < len(d.Runes); {
		r := d.Runes[i]
		if r == '\n' {
			lineOp(i)
			i++
			continue
		}
		a := d.Attrs[i]
		if a.Atom != nil && a.Atom.Kind == AtomBroadcast {
			// Slack's mention op: the label as text, slackmention says what
			// it is (NativeAtChannel and bChannel look for id "BKchannel")
			label := "@" + a.Atom.ID
			add(map[string]any{"insert": label, "attributes": map[string]any{
				"slackmention": map[string]any{"id": broadcastRowID(a.Atom.ID), "label": label}}}, i, i+1)
			i++
			continue
		}
		if a.Atom != nil && a.Atom.Kind == AtomMessage {
			// Slack's message slug op: the label, slackslug says what it is
			add(map[string]any{"insert": a.Atom.Text, "attributes": map[string]any{"slackslug": messageSlugDelta(a.Atom)}}, i, i+1)
			i++
			continue
		}
		if a.Atom != nil && a.Atom.Kind == AtomLink {
			// a link slug op: the label, slackslug the link
			add(map[string]any{"insert": a.Atom.Text, "attributes": map[string]any{"slackslug": linkSlugDelta(a.Atom)}}, i, i+1)
			i++
			continue
		}
		if a.Atom != nil {
			add(map[string]any{"insert": deltaEmbed(a.Atom)}, i, i+1)
			i++
			continue
		}
		j := i + 1
		for j < len(d.Runes) && d.Runes[j] != '\n' && d.Attrs[j].Atom == nil &&
			d.Attrs[j].Style == a.Style && d.Attrs[j].Link == a.Link {
			j++
		}
		if d.Lines != nil && line < len(d.Lines) && d.Lines[line].Kind == Divider {
			i = j // a divider line's text is not content
			continue
		}
		op := map[string]any{"insert": string(d.Runes[i:j])}
		if at := deltaAttrs(a); at != nil {
			op["attributes"] = at
		}
		add(op, i, j)
		i = j
	}
	lineOp(len(d.Runes)) // Quill documents always end with a newline
	return ops, sp
}

func deltaAttrs(a Attr) map[string]any {
	if a.Style == 0 && a.Link == "" {
		return nil
	}
	at := map[string]any{}
	if a.Style&Bold != 0 {
		at["bold"] = true
	}
	if a.Style&Italic != 0 {
		at["italic"] = true
	}
	if a.Style&Underline != 0 {
		at["underline"] = true
	}
	if a.Style&Strike != 0 {
		at["strike"] = true
	}
	if a.Style&Code != 0 {
		at["code"] = true
	}
	if a.Style&Unlink != 0 {
		at["unlink"] = true
	}
	if a.Link != "" {
		at["link"] = a.Link
	}
	return at
}

func deltaEmbed(a *Atom) map[string]any {
	v := map[string]any{"id": a.ID, "label": a.Text}
	switch a.Kind {
	case AtomUser:
		return map[string]any{"slackuser": v}
	case AtomChannel:
		return map[string]any{"slackchannel": v}
	case AtomUsergroup:
		return map[string]any{"slackusergroup": v}
	case AtomEmoji:
		e := map[string]any{"name": a.ID}
		if a.Uni != "" {
			e["unicode"] = a.Uni
		}
		return map[string]any{"slackemoji": e}
	}
	return map[string]any{"text": a.Text}
}
