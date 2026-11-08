package slack

import (
	"errors"
	"net/url"
)

// dpopKey is a per-domain DPoP key. Binding isn't implemented yet: token
// sets that ask for DPoP sign in unbound, as Slack desktop does when it has
// no secure key storage.
type dpopKey struct{}

func loadOrCreateDPoPKey(dir, domain string) (*dpopKey, error) { return nil, nil }

func (k *dpopKey) proof(method string, u *url.URL, withJWK bool) (string, error) {
	return "", errors.New("dpop: not implemented")
}
