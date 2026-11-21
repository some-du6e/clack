package messages

import "github.com/some-du6e/clack/internal/core"

// withStopped marks m stopped by id, and rowStopped reads the label the
// row hands richview. The red-evidence commit replaces this file with a
// stub (no label).
func withStopped(m core.Message, id string) core.Message {
	m.Stopped, m.StoppedBy = true, id
	return m
}

func rowStopped(r *row) (bool, string) {
	o := r.rvOpts()
	return o.Stopped, o.StoppedBy
}
