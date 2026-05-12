package tagsuggest

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

type TranscriberConfig struct {
	Binary  string
	Args    []string
	Timeout time.Duration
}

type Transcriber struct {
	binary  string
	args    []string
	timeout time.Duration
}

func NewTranscriber(config TranscriberConfig) *Transcriber {
	binary := strings.TrimSpace(config.Binary)
	if binary == "" {
		return nil
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}

	args := make([]string, 0, len(config.Args))
	for _, arg := range config.Args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		args = append(args, trimmed)
	}

	return &Transcriber{
		binary:  binary,
		args:    args,
		timeout: timeout,
	}
}

func (t *Transcriber) Enabled() bool {
	return t != nil && t.binary != ""
}

func (t *Transcriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	if !t.Enabled() {
		return "", ErrDisabled
	}

	audioPath = strings.TrimSpace(audioPath)
	if audioPath == "" {
		return "", errors.New("audio path is required")
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		runCtx, cancel = context.WithTimeout(ctx, t.timeout)
		defer cancel()
	}

	args := make([]string, 0, len(t.args)+1)
	foundPlaceholder := false
	for _, arg := range t.args {
		if strings.Contains(arg, "{input}") {
			args = append(args, strings.ReplaceAll(arg, "{input}", audioPath))
			foundPlaceholder = true
			continue
		}
		args = append(args, arg)
	}
	if !foundPlaceholder {
		args = append(args, audioPath)
	}

	cmd := exec.CommandContext(runCtx, t.binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", errors.New(message)
	}

	transcript := compactTranscript(stdout.String())
	if transcript == "" {
		return "", errors.New("empty transcript output")
	}
	return transcript, nil
}

func compactTranscript(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return ""
	}

	joined := strings.Join(fields, " ")
	if len(joined) > 1200 {
		return strings.TrimSpace(joined[:1200])
	}
	return joined
}
