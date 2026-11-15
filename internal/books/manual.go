package books

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

// ManualKeyPrefix starts the key of a book that was added by hand, with no
// match on Open Library, Hardcover or Google Books ("manual:3f9a1c0d27b4").
// Such a book is searched for, downloaded and imported like any other, but
// nothing is ever looked up for it again, so what the person typed stays.
const ManualKeyPrefix = "manual:"

// IsManualKey reports whether key belongs to a book added by hand.
func IsManualKey(key string) bool { return strings.HasPrefix(strings.TrimSpace(key), ManualKeyPrefix) }

// Manual reports whether the book was added by hand.
func (b Book) Manual() bool { return IsManualKey(b.OLKey) }

// manualExternalID is what an outside id may look like: letters, digits and a
// few marks, so it is safe in a key, a log line and a link.
var manualExternalID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ErrBadExternalID is returned for an outside id with odd characters or that is too long.
var ErrBadExternalID = errors.New("an id can only have letters, digits, dots, dashes and underscores, up to 64 characters")

// NewManualKey makes the key for a book added by hand. An outside id the
// person typed (an ISBN, an Audible ASIN, an Open Library key) is used as it
// is, so adding the same one twice is caught; without one the key is random.
func NewManualKey(externalID string) (string, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID != "" {
		if !manualExternalID.MatchString(externalID) {
			return "", ErrBadExternalID
		}
		return ManualKeyPrefix + externalID, nil
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return ManualKeyPrefix + hex.EncodeToString(b[:]), nil
}
