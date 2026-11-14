package emoji

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"io"
	"sync"
)

// keywords.json.gz is Slack's English emoji keyword map
// (getEmojiKeywordsMap, module 52eK of Slack's web client, build
// efbd526e8fb3), which loadEmojiTranslations registers for the default
// locale and the ":" autocomplete matches (entities.emoji.matchKeywords).
// It is one row per standard emoji, [name, keyword...], in the order
// getConsolidatedEmoji lists them (the emoji_v17 table, which holds every
// name in standard.json.gz): 123 KB of JSON, 35 KB gzipped.
//
//go:embed keywords.json.gz
var keywordsGz []byte

var (
	kwOnce   sync.Once
	kwByName map[string][]string
	kwOrder  map[string]int
)

func loadKeywords() {
	kwByName, kwOrder = map[string][]string{}, map[string]int{}
	r, err := gzip.NewReader(bytes.NewReader(keywordsGz))
	if err != nil {
		return
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return
	}
	var rows [][]string
	if json.Unmarshal(b, &rows) != nil {
		return
	}
	for i, row := range rows {
		if len(row) == 0 {
			continue
		}
		kwOrder[row[0]] = i
		if len(row) > 1 {
			kwByName[row[0]] = row[1:]
		}
	}
}

// Keywords returns Slack's keywords for a standard emoji's primary name
// (nil: it has none). Aliases and custom emoji have none.
func Keywords(name string) []string {
	kwOnce.Do(loadKeywords)
	return kwByName[name]
}

// SlackOrder is a standard emoji's place in Slack's consolidated emoji
// list (getConsolidatedEmoji), -1 when Slack doesn't list the name. The
// ":" autocomplete keeps this order among rows it can't tell apart.
func SlackOrder(name string) int {
	kwOnce.Do(loadKeywords)
	if i, ok := kwOrder[name]; ok {
		return i
	}
	return -1
}
