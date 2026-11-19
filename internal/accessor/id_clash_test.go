package accessor

import (
	"context"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A new meme whose ID clashes with an existing one must fail without
// touching the existing meme. Create used to open the stored file with
// os.Create, which truncated the existing meme's file and wrote the new
// upload into it; the database then refused the duplicate ID and the cleanup
// deleted the file, so the earlier meme lost its file and thumbnail.
func TestPostgresCreateWithClashingIDLeavesExistingMemeIntact(t *testing.T) {
	store := newRegressionStore(t)
	token := randToken(t)

	original, err := store.Create(context.Background(), CreateInput{
		File:     strings.NewReader("original bytes " + token),
		Header:   textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename: "original-" + token + ".png",
	})
	if err != nil {
		t.Fatalf("Create original: %v", err)
	}
	originalPath := filepath.Join(store.UploadDir(), original.StoredName)

	previous := newMemeID
	newMemeID = func() string { return original.ID }
	t.Cleanup(func() { newMemeID = previous })

	if _, err := store.Create(context.Background(), CreateInput{
		File:     strings.NewReader("clashing bytes " + token),
		Header:   textproto.MIMEHeader{"Content-Type": []string{"image/png"}},
		Filename: "clash-" + token + ".png",
	}); err == nil {
		t.Fatal("Create with an ID that is already in use succeeded")
	}

	data, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("existing meme's file is gone: %v", err)
	}
	if string(data) != "original bytes "+token {
		t.Fatalf("existing meme's file was overwritten: %q", data)
	}
	saved, err := store.GetAnyByID(context.Background(), original.ID)
	if err != nil {
		t.Fatalf("existing meme row is gone: %v", err)
	}
	if saved.OriginalName != original.OriginalName {
		t.Fatalf("existing meme row changed: %q, want %q", saved.OriginalName, original.OriginalName)
	}
}
