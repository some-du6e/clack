package slack

// The new APIs the counting tests use, through helpers (the red tree's
// shims stub them).

func setAPIHealthHooks(c *Client, clientErrors, serviceErrors func()) {
	c.SetAPIHealthHooks(clientErrors, serviceErrors)
}

func resetAPIErrors(c *Client) { c.ResetAPIErrors() }

func apiErrors(c *Client) (client, service int) { return c.APIErrors() }
