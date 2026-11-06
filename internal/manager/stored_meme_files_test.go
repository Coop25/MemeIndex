package manager

import "testing"

// A store that cannot enumerate its files must make StoredMemeFiles fail, so
// the storage orphan scan fails closed instead of treating every file as
// unreferenced.
func TestStoredMemeFilesErrorsWithoutEnumeration(t *testing.T) {
	m := NewMemeManager(&adminReadCacheStore{})
	files, err := m.StoredMemeFiles()
	if err == nil {
		t.Fatalf("StoredMemeFiles = %v, want an error", files)
	}
}
