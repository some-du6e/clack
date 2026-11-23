package taut

// withUploadFields is f with its own upload fields (taut.File).
func withUploadFields(f File, subtype string, duration float64, wave []float64) File {
	f.Subtype, f.Duration, f.AudioWaveSamples = subtype, duration, wave
	return f
}

// uploadFields is f's own upload fields.
func uploadFields(f File) (string, float64, []float64) {
	return f.Subtype, f.Duration, f.AudioWaveSamples
}
