package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"memeindex/internal/accessor"
)

const maxDirectMediaBytes int64 = 256 << 20

func isDirectMediaURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp", ".tif", ".tiff", ".mp4", ".webm", ".mov", ".m4v", ".mkv", ".avi":
		return true
	}
	return false
}

// Validate and dial the same resolved IP so DNS changes cannot bypass the
// private-network restriction. Redirects use this transport too.
func directMediaHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, address := range addresses {
				if err := validateRemoteURL(ctx, "http://"+net.JoinHostPort(address.IP.String(), port)); err != nil {
					return nil, err
				}
			}
			var lastErr error = errors.New("remote host did not resolve")
			for _, address := range addresses {
				conn, err := (&net.Dialer{Timeout: 20 * time.Second}).DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return validateRemoteURL(req.Context(), req.URL.String())
		},
	}
}

func (s *Server) createMemeFromDirectURL(ctx context.Context, actor accessor.AuditActor, sourceURL string, tags []string, notes string) (accessor.Meme, error) {
	if err := validateRemoteURL(ctx, sourceURL); err != nil {
		return accessor.Meme{}, err
	}
	client := directMediaHTTPClient()
	defer client.CloseIdleConnections()
	return s.importDirectMedia(ctx, client, actor, sourceURL, tags, notes)
}

func (s *Server) importDirectMedia(ctx context.Context, client *http.Client, actor accessor.AuditActor, sourceURL string, tags []string, notes string) (accessor.Meme, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return accessor.Meme{}, err
	}
	response, err := client.Do(req)
	if err != nil {
		return accessor.Meme{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound {
			return accessor.Meme{}, errors.New("the attachment is unavailable or its link has expired; copy a fresh attachment link and try again")
		}
		return accessor.Meme{}, fmt.Errorf("media download returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxDirectMediaBytes {
		return accessor.Meme{}, errors.New("linked media exceeds the 256 MB limit")
	}
	// Finish the download before storing, so oversized or interrupted responses
	// never become partial entries in the index.
	file, err := os.CreateTemp("", "memeindex-direct-*")
	if err != nil {
		return accessor.Meme{}, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	n, err := io.Copy(file, io.LimitReader(response.Body, maxDirectMediaBytes+1))
	if err != nil {
		return accessor.Meme{}, err
	}
	if n > maxDirectMediaBytes {
		return accessor.Meme{}, errors.New("linked media exceeds the 256 MB limit")
	}
	if n == 0 {
		return accessor.Meme{}, errors.New("linked media is empty")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return accessor.Meme{}, err
	}
	prefix := make([]byte, 512)
	count, err := file.Read(prefix)
	if err != nil && err != io.EOF {
		return accessor.Meme{}, err
	}
	contentType := http.DetectContentType(prefix[:count])
	if contentType == "application/octet-stream" {
		contentType, _, _ = mime.ParseMediaType(response.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(contentType, "image/") && !strings.HasPrefix(contentType, "video/") {
		return accessor.Meme{}, errors.New("the link did not return an image or video")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return accessor.Meme{}, err
	}
	filename := path.Base(req.URL.Path)
	if response.Request != nil && response.Request.URL != nil {
		filename = path.Base(response.Request.URL.Path)
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", contentType)
	return s.managers.CreateMemeAsWithSource(actor, file, header, filename, tags, notes, sourceURL)
}
