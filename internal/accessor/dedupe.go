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
	Existing Meme
}

func (e *DuplicateMemeError) Error() string {
	if e == nil {
		return "duplicate meme"
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
