//go:build darwin && !cgo

package spell

func platformOpen(langs []string, dir string) ([]Engine, error) {
	return openHunspellAll(langs, dir)
}

func platformThreadInit() {}

func platformLocale() string { return "" }

const SystemLanguages = false
