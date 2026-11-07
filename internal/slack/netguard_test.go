package slack

import (
	"net/netip"
	"testing"
)

// Credential-free fetches never dial loopback, private, link-local (cloud
// metadata) or other non-public addresses.
func TestDialControlRefusesPrivate(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:443":               false,
		"127.8.9.10:80":               false,
		"[::1]:443":                   false,
		"169.254.169.254:80":          false, // cloud metadata
		"10.1.2.3:443":                false,
		"172.16.0.1:443":              false,
		"172.31.255.255:443":          false,
		"192.168.1.1:443":             false,
		"100.64.0.1:443":              false,
		"0.0.0.0:443":                 false,
		"[fe80::1]:443":               false,
		"[fc00::1]:443":               false,
		"[fd12:3456::1]:443":          false,
		"[::ffff:127.0.0.1]:443":      false,
		"[::ffff:169.254.169.254]:80": false,
		"[::]:443":                    false,
		"1.1.1.1:443":                 true,
		"151.101.1.140:443":           true,
		"[2606:4700::1111]:443":       true,
		"172.32.0.1:443":              true,
	} {
		err := dialControl("tcp", addr, nil)
		if (err == nil) != ok {
			t.Errorf("dialControl(%s) = %v, want ok=%v", addr, err, ok)
		}
	}
	if PublicIP(netip.Addr{}) {
		t.Error("zero addr is public")
	}
}
