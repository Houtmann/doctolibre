package notify

import (
	"net/netip"
	"testing"
)

func TestForbiddenIP(t *testing.T) {
	t.Parallel()
	for ip, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "10.1.2.3": true, "172.16.0.1": true, "192.168.1.10": true,
		"169.254.169.254": true, "fe80::1": true, "fd00::1": true, "0.0.0.0": true, "100.64.0.1": true,
		"::ffff:127.0.0.1": true, "224.0.0.1": true, "255.255.255.255": true,
		"8.8.8.8": false, "93.184.216.34": false, "2606:4700:4700::1111": false,
	} {
		if got := forbiddenIP(netip.MustParseAddr(ip)); got != want {
			t.Errorf("forbiddenIP(%s) = %v, attendu %v", ip, got, want)
		}
	}
}
