package slack

import "context"

// Red-tree shims: the new APIs the tests use don't exist on the old code.

func setAPIHealthHooks(c *Client, clientErrors, serviceErrors func()) {}

func resetAPIErrors(c *Client) {}

func apiErrors(c *Client) (client, service int) { return 0, 0 }

func setAwaitOnline(r *RTM, f func(ctx context.Context) bool) {}
