package vpn

import "context"

// TestPublicIPFrom re-exports the unexported publicIPFrom for
// tunnel_test.go (package vpn_test) — lets that test point at a local
// fixture server instead of the real api.ipify.org, without adding a
// testing-only parameter to the public PublicIP API.
func (t *Tunnel) TestPublicIPFrom(ctx context.Context, url string) (string, error) {
	return t.publicIPFrom(ctx, url)
}
