package spell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/some-du6e/clack/internal/slack/tlsfp"
	"github.com/some-du6e/clack/internal/spell/hunspell"
)

// Hunspell engines read Chromium's .bdic dictionaries. Like the desktop
// app (Chromium's SpellcheckHunspellDictionary), a dictionary missing
// from the dictionary folder is downloaded from DictionaryURL + file with
// a plain GET: no cookie or token, it's a public static file.

type hunspellEngine struct{ d *hunspell.Dict }

func (h hunspellEngine) Check(w string) bool       { return h.d.Check(w) }
func (h hunspellEngine) Suggest(w string) []string { return h.d.Suggest(w) }
func (h hunspellEngine) Add(w string)              { h.d.Add(w) }
func (h hunspellEngine) Close()                    {}

// DictionaryDir is <user cache>/clack/Dictionaries ("" when the OS has
// no cache folder), Chromium's "Dictionaries" folder.
func DictionaryDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "clack", "Dictionaries")
}

// UserDir is where Custom Dictionary.txt lives: <user config>/clack.
func UserDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "clack")
}

// fetchDict is the download (a var for tests).
var fetchDict = func(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := tlsfp.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("spell: %s: %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 32<<20))
}

// openHunspell loads (downloading if needed) lang's dictionary.
func openHunspell(lang, dir string) (Engine, error) {
	file := BDicFile(lang)
	var data []byte
	path := ""
	if dir != "" {
		path = filepath.Join(dir, file)
		data, _ = os.ReadFile(path)
	}
	if data != nil {
		if d, err := hunspell.ParseBDic(data); err == nil {
			return hunspellEngine{d}, nil
		}
		data = nil // corrupt: download again
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	data, err := fetchDict(ctx, DictionaryURL+file)
	if err != nil {
		return nil, err
	}
	d, err := hunspell.ParseBDic(data)
	if err != nil {
		return nil, fmt.Errorf("spell: %s: %w", file, err)
	}
	if path != "" {
		if err := os.MkdirAll(dir, 0o700); err == nil {
			tmp := path + ".tmp"
			if os.WriteFile(tmp, data, 0o600) == nil {
				_ = os.Rename(tmp, path)
			}
		}
	}
	return hunspellEngine{d}, nil
}

// openHunspellAll opens every language's dictionary; the ones that fail
// are left out and reported.
func openHunspellAll(langs []string, dir string) ([]Engine, error) {
	var out []Engine
	var errs []error
	for _, l := range langs {
		e, err := openHunspell(l, dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, e)
	}
	return out, errors.Join(errs...)
}
