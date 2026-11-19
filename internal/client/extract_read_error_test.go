package client

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"testing"
)

// failAfterReader yields the first n bytes of data, then err: a transfer that
// breaks partway, such as a migration source whose connection is reset.
type failAfterReader struct {
	data []byte
	n    int
	err  error
}

func (r *failAfterReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, r.err
	}
	if len(p) > r.n {
		p = p[:r.n]
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	r.n -= n
	return n, nil
}

// A backup stream that fails partway through a file's contents is a problem
// with the backup or its transfer, not with this server, and has to be
// reported to the admin as one. Failing while reading an entry's header
// already was; failing inside its contents fell through as an internal error,
// so the restore answered "the server could not complete the restore" (500)
// and a migration said "the restore failed on this server".
func TestExtractReportsMidFileReadFailureAsDamagedArchive(t *testing.T) {
	// Incompressible, so the cut lands well inside this file's data.
	random := make([]byte, 256<<10)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range random {
		random[i] = byte(rng.Uint32())
	}
	resetArchive := makeTestArchive(t, map[string]string{"uploads/big.bin": string(random)})

	// Compressible, so flipped bytes break the deflate stream itself.
	var text strings.Builder
	for i := 0; text.Len() < 256<<10; i++ {
		fmt.Fprintf(&text, "line %d of a meme caption that compresses well\n", i)
	}
	corrupt := makeTestArchive(t, map[string]string{"uploads/big.txt": text.String()})
	for i := len(corrupt) / 2; i < len(corrupt)/2+64; i++ {
		corrupt[i] ^= 0xa5
	}

	cases := map[string]io.Reader{
		"connection reset": &failAfterReader{data: resetArchive, n: len(resetArchive) / 2, err: errors.New("read tcp 10.0.0.2:443: connection reset by peer")},
		"corrupt data":     bytes.NewReader(corrupt),
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			err := extractPortableArchive(source, t.TempDir())
			if err == nil {
				t.Fatal("extract succeeded on a broken stream")
			}
			var friendly *userFacingError
			if !errors.As(err, &friendly) {
				t.Fatalf("extract error %q is not reported to the admin as a problem with the backup", err)
			}
		})
	}
}
