package accessor

import "github.com/google/uuid"

// NewID returns a fresh identifier for a stored record: a meme, an audiobook
// and its upload, a bookmark or a series. Meme and audiobook IDs also name
// their files on disk, so two records must never get the same one.
//
// It is a UUIDv7: the creation time in milliseconds, a counter that keeps IDs
// from one process increasing, and 62 random bits. It used to be
// uuid.NewV6, but google/uuid v1.6.0's NewV6 writes the 60-bit timestamp
// over the whole first eight bytes and then overwrites four of its bits with
// the version number, so two IDs made a multiple of about 0.4 ms apart, within
// roughly 6.5 ms of each other, come out identical. A colliding meme ID made
// Create write the new upload over the existing meme's file and then, when the
// database refused the duplicate ID, delete it.
func NewID() string {
	return uuid.Must(uuid.NewV7()).String()
}

// newMemeID is NewID for new memes. A test swaps it to force a clash with a
// meme that already exists.
var newMemeID = NewID
