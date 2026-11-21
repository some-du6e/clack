package core

import "context"

// joinCodeChannel reaches App.JoinCodeChannel for code_join_test.go (a
// red-evidence commit stubs it where the method isn't).
func joinCodeChannel(a *App, ctx context.Context, ch, origin string) error {
	return a.JoinCodeChannel(ctx, ch, origin)
}
