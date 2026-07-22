package client

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestValidateRemoteURLRejectsUnsafeTargets(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"file:///etc/passwd",
		"http://127.0.0.1/video",
		"http://[::1]/video",
		"http://169.254.169.254/latest/meta-data",
		"http://10.1.2.3/video",
		"http://172.16.0.1/video",
		"http://192.168.1.10/video",
		"https://user:password@example.com/video",
	} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if err := validateRemoteURL(context.Background(), raw); err == nil {
				t.Fatalf("validateRemoteURL(%q) unexpectedly succeeded", raw)
			}
		})
	}
}

func TestUntrustedUploadHeadersForceRiskyFilesToDownload(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	setUntrustedAssetHeaders(recorder, "/uploads/untrusted.svg")
	if got := recorder.Header().Get("Content-Disposition"); got != "attachment" {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

func TestValidateRemoteURLAllowsPublicHTTPAddress(t *testing.T) {
	t.Parallel()
	if err := validateRemoteURL(context.Background(), "https://8.8.8.8/video"); err != nil {
		t.Fatalf("validateRemoteURL() returned %v", err)
	}
}

func TestSafeLinkFilename(t *testing.T) {
	t.Parallel()
	if got := safeLinkFilename(`  a/b:c*?"<>|  `); got != "a-b-c------" {
		t.Fatalf("safeLinkFilename() = %q", got)
	}
	if got := safeLinkFilename("..."); got != "saved-link" {
		t.Fatalf("safeLinkFilename(empty) = %q", got)
	}
}
