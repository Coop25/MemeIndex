package accessor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

type DuplicateMemeError struct {
	// Existing is the meme already holding this content. It is left empty
	// when PendingDelete is set, so a hidden meme's details never leak back to
	// the uploader.
	Existing Meme
	// PendingDelete is set when the existing copy is hidden from the app
	// because someone asked to delete it and that request is still waiting
	// for approval.
	PendingDelete bool
}

// removeUploadAndThumbnail deletes a stored upload and its generated
// thumbnail (if any), ignoring files that are already gone. Used to clean up
// after an upload that fails or turns out to be a duplicate.
func removeUploadAndThumbnail(uploadDir, previewDir, storedName string) {
	_ = os.Remove(filepath.Join(uploadDir, storedName))
	if previewDir != "" {
		_ = os.Remove(filepath.Join(previewDir, thumbnailFileName(storedName)))
	}
}

func (e *DuplicateMemeError) Error() string {
	if e == nil {
		return "duplicate meme"
	}
	if e.PendingDelete {
		return "duplicate meme: the existing copy is awaiting delete approval"
	}
	if e.Existing.ID != "" {
		return fmt.Sprintf("duplicate meme: %s", e.Existing.ID)
	}
	return "duplicate meme"
}

func newContentHashWriter() hash.Hash {
	return sha256.New()
}

func contentHashString(hasher hash.Hash) string {
	return hex.EncodeToString(hasher.Sum(nil))
}

func computeFileHash(filePath string) (string, error) {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := newContentHashWriter()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return contentHashString(hasher), nil
}
