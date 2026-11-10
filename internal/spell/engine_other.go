//go:build !darwin && !windows

package spell

// Linux and the others: Hunspell dictionaries, as Chromium does there.
func platformOpen(langs []string, dir string) ([]Engine, error) {
	return openHunspellAll(langs, dir)
}

func platformThreadInit() {}

func platformLocale() string { return "" }

// SystemLanguages reports whether the system picks the languages (macOS);
// elsewhere Preferences lists them.
const SystemLanguages = false
