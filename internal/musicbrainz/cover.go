package musicbrainz

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxCoverBytes bounds a downloaded cover; a 500 pixel front cover is a few
// tens of kilobytes.
const maxCoverBytes = 8 << 20

// WithCoverArtBaseURL points the client at another Cover Art Archive (a
// mirror, or a test fake).
func WithCoverArtBaseURL(u string) Option {
	return func(c *Client) { c.coverBase = strings.TrimRight(u, "/") }
}

// FrontCover downloads the front cover of a release group at 500 pixels from
// the Cover Art Archive (which redirects to the image) and returns the image
// bytes with their content type ("image/jpeg" or "image/png"). It returns
// ErrNotFound for a release group without cover art. The Cover Art Archive
// has no request limit of its own, so this does not use the MusicBrainz rate
// limit.
func (c *Client) FrontCover(ctx context.Context, releaseGroupID string) ([]byte, string, error) {
	if strings.TrimSpace(releaseGroupID) == "" {
		return nil, "", ErrNotFound
	}
	u := c.coverBase + "/release-group/" + url.PathEscape(releaseGroupID) + "/front-500"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", fmt.Errorf("cover art: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("cover art: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, "", ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, "", fmt.Errorf("cover art: unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("cover art: read image: %w", err)
	}
	if len(data) > maxCoverBytes {
		return nil, "", fmt.Errorf("cover art: image is larger than %d bytes", maxCoverBytes)
	}
	// Trust the bytes, not the header: only real JPEG and PNG images are kept.
	switch ct := http.DetectContentType(data); ct {
	case "image/jpeg", "image/png":
		return data, ct, nil
	default:
		return nil, "", fmt.Errorf("cover art: not a JPEG or PNG image (%s)", ct)
	}
}
