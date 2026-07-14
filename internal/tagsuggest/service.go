package tagsuggest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"
)

var (
	ErrDisabled        = errors.New("tag suggestions are disabled")
	ErrUnavailable     = errors.New("tag suggestion service is unavailable")
	ErrUnsupported     = errors.New("tag suggestions are only supported for images and videos with generated thumbnails")
	ErrInvalidResponse = errors.New("tag suggestion model returned an unusable response")
)

type Config struct {
	OllamaURL    string
	Model        string
	Timeout      time.Duration
	MaxTags      int
	GenerateOnly bool
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.OllamaURL) != "" && strings.TrimSpace(c.Model) != ""
}

type Service struct {
	baseURL      string
	model        string
	maxTags      int
	timeout      time.Duration
	generateOnly bool
	httpClient   *http.Client
}

type Request struct {
	AssetPaths   []string
	Filename     string
	ContentType  string
	Source       string
	Transcript   string
	ExistingTags []string
	KnownTags    []string
}

type Result struct {
	Tags   []string `json:"tags"`
	Model  string   `json:"model"`
	Source string   `json:"source"`
}

func New(config Config) *Service {
	if !config.Enabled() {
		return nil
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}

	maxTags := config.MaxTags
	if maxTags <= 0 {
		maxTags = 8
	}

	return &Service{
		baseURL:      strings.TrimRight(strings.TrimSpace(config.OllamaURL), "/"),
		model:        strings.TrimSpace(config.Model),
		maxTags:      maxTags,
		timeout:      timeout,
		generateOnly: config.GenerateOnly,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.baseURL != "" && s.model != ""
}

func (s *Service) Model() string {
	if s == nil {
		return ""
	}
	return s.model
}

func (s *Service) Timeout() time.Duration {
	if s == nil || s.timeout <= 0 {
		return 45 * time.Second
	}
	return s.timeout
}

func (s *Service) WaitUntilReady(ctx context.Context) error {
	if !s.Enabled() {
		return ErrDisabled
	}

	for {
		ready, err := s.ready(ctx)
		if err == nil && ready {
			return nil
		}

		select {
		case <-ctx.Done():
			if err != nil {
				return err
			}
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *Service) ready(ctx context.Context) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/api/tags", nil)
	if err != nil {
		return false, err
	}

	response, err := s.httpClient.Do(request)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, fmt.Errorf("%w: %s", ErrUnavailable, response.Status)
	}

	var payload struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return false, fmt.Errorf("%w: invalid readiness payload", ErrUnavailable)
	}

	target := strings.ToLower(strings.TrimSpace(s.model))
	for _, model := range payload.Models {
		if strings.ToLower(strings.TrimSpace(model.Name)) == target || strings.ToLower(strings.TrimSpace(model.Model)) == target {
			return true, nil
		}
	}

	return false, fmt.Errorf("%w: model %q is not pulled yet", ErrUnavailable, s.model)
}

func (s *Service) Suggest(ctx context.Context, input Request) (Result, error) {
	if !s.Enabled() {
		return Result{}, ErrDisabled
	}

	imagePayloads := make([]string, 0, len(input.AssetPaths))
	for _, assetPath := range input.AssetPaths {
		trimmedPath := strings.TrimSpace(assetPath)
		if trimmedPath == "" {
			continue
		}
		imageBytes, err := os.ReadFile(trimmedPath)
		if err != nil {
			return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		imagePayloads = append(imagePayloads, base64.StdEncoding.EncodeToString(imageBytes))
	}
	if len(imagePayloads) == 0 {
		return Result{}, ErrUnsupported
	}

	if s.generateOnly {
		return s.suggestWithGenerateFallback(ctx, input, imagePayloads, nil)
	}

	result, err := s.suggestWithStructuredChat(ctx, input, imagePayloads)
	if err != nil {
		// Fallback: some local vision models fail on chat+schema even when plain generation works.
		result, err = s.suggestWithGenerateFallback(ctx, input, imagePayloads, err)
	}
	if err != nil {
		return Result{}, err
	}

	return result, nil
}

func (s *Service) suggestWithStructuredChat(ctx context.Context, input Request, imagePayloads []string) (Result, error) {
	schema := tagResponseSchema(s.maxTags)

	knownTags := promptKnownTags(input.KnownTags)

	existingTags := normalizeTags(input.ExistingTags)
	prompt := buildPrompt(input, knownTags, existingTags, s.maxTags)
	body := map[string]any{
		"model":  s.model,
		"stream": false,
		"format": schema,
		"options": map[string]any{
			"temperature": 0,
			"num_predict": 256,
		},
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": "You suggest concise organizer tags for memes. Return only valid JSON that matches the requested schema.",
			},
			{
				"role":    "user",
				"content": prompt,
				"images":  imagePayloads,
			},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := s.httpClient.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		if len(message) > 500 {
			message = message[:500]
		}
		return Result{}, fmt.Errorf("%w: %s", ErrUnavailable, message)
	}

	var chatResponse struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(responseBody, &chatResponse); err != nil {
		return Result{}, fmt.Errorf("%w: invalid response payload", ErrUnavailable)
	}

	tags, err := extractTagsFromModelText(chatResponse.Message.Content)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	result := s.buildResult(input, tags)
	if len(result.Tags) == 0 {
		return Result{}, fmt.Errorf("%w: all returned tags were empty, duplicate, or low-value", ErrInvalidResponse)
	}
	return result, nil
}

func (s *Service) suggestWithGenerateFallback(ctx context.Context, input Request, imagePayloads []string, originalErr error) (Result, error) {
	prompt := buildPrompt(input, promptKnownTags(input.KnownTags), normalizeTags(input.ExistingTags), s.maxTags)

	body := map[string]any{
		"model":  s.model,
		"prompt": prompt,
		"images": imagePayloads,
		"stream": false,
		"format": tagResponseSchema(s.maxTags),
		"options": map[string]any{
			"temperature": 0,
			"num_predict": 256,
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/generate", bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := s.httpClient.Do(request)
	if err != nil {
		if originalErr == nil {
			return Result{}, fmt.Errorf("%w: generate-only request failed: %v", ErrUnavailable, err)
		}
		return Result{}, fmt.Errorf("%w: structured path failed (%v); fallback generate also failed: %v", ErrUnavailable, originalErr, err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		if originalErr == nil {
			return Result{}, fmt.Errorf("%w: generate-only read failed: %v", ErrUnavailable, err)
		}
		return Result{}, fmt.Errorf("%w: fallback generate read failed: %v", ErrUnavailable, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		if len(message) > 500 {
			message = message[:500]
		}
		if originalErr == nil {
			return Result{}, fmt.Errorf("%w: generate-only request failed: %s", ErrUnavailable, message)
		}
		return Result{}, fmt.Errorf("%w: structured path failed (%v); fallback generate failed: %s", ErrUnavailable, originalErr, message)
	}

	var generateResponse struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(responseBody, &generateResponse); err != nil {
		if originalErr == nil {
			return Result{}, fmt.Errorf("%w: generate-only returned invalid payload", ErrUnavailable)
		}
		return Result{}, fmt.Errorf("%w: fallback generate returned invalid payload", ErrUnavailable)
	}

	tags, err := extractTagsFromModelText(generateResponse.Response)
	if err != nil {
		if originalErr == nil {
			return Result{}, fmt.Errorf("%w: generate-only parse failed: %v", ErrInvalidResponse, err)
		}
		return Result{}, fmt.Errorf("%w: structured path failed (%v); fallback parse failed: %v", ErrInvalidResponse, originalErr, err)
	}

	result := s.buildResult(input, tags)
	if len(result.Tags) == 0 {
		return Result{}, fmt.Errorf("%w: all returned tags were empty, duplicate, or low-value", ErrInvalidResponse)
	}
	return result, nil
}

func tagResponseSchema(maxTags int) map[string]any {
	if maxTags <= 0 {
		maxTags = 8
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"tags": map[string]any{
				"type":     "array",
				"minItems": 1,
				"maxItems": maxTags,
				"items": map[string]any{
					"type":      "string",
					"minLength": 3,
					"maxLength": 48,
				},
			},
		},
		"required": []string{"tags"},
	}
}

func promptKnownTags(tags []string) []string {
	const maxKnownTags = 60
	normalized := normalizeTags(tags)
	if len(normalized) > maxKnownTags {
		normalized = normalized[:maxKnownTags]
	}
	return normalized
}

func (s *Service) buildResult(input Request, rawTags []string) Result {
	existingTags := normalizeTags(input.ExistingTags)
	tags := normalizeTags(rawTags)
	tags = filterLowValueTags(tags, input)
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if slices.Contains(existingTags, tag) {
			continue
		}
		filtered = append(filtered, tag)
		if len(filtered) >= s.maxTags {
			break
		}
	}

	return Result{
		Tags:   filtered,
		Model:  s.model,
		Source: strings.TrimSpace(input.Source),
	}
}

func extractTagsFromModelText(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty model response")
	}

	for index := 0; index < len(trimmed); index++ {
		if trimmed[index] != '{' && trimmed[index] != '[' {
			continue
		}
		var value json.RawMessage
		decoder := json.NewDecoder(strings.NewReader(trimmed[index:]))
		if err := decoder.Decode(&value); err != nil {
			continue
		}
		if tags := tagsFromJSON(value); len(tags) > 0 {
			return tags, nil
		}
	}

	plain := strings.TrimSpace(strings.Trim(trimmed, "`"))
	lowerPlain := strings.ToLower(plain)
	if strings.HasPrefix(lowerPlain, "tags:") {
		plain = strings.TrimSpace(plain[len("tags:"):])
	} else if strings.ContainsAny(plain, "{}[]:\"") {
		return nil, errors.New("malformed JSON response")
	}
	parts := strings.FieldsFunc(plain, func(r rune) bool {
		return r == '\n' || r == ',' || r == ';'
	})
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned := strings.TrimSpace(strings.TrimLeftFunc(part, func(r rune) bool {
			return unicode.IsDigit(r) || strings.ContainsRune("-*.) (•", r)
		}))
		words := strings.Fields(strings.ToLower(cleaned))
		if len(words) > 3 || (len(words) > 0 && slices.Contains([]string{"here", "these", "suggested", "because", "explanation"}, words[0])) {
			continue
		}
		if tag, ok := sanitizeTag(cleaned); ok {
			tags = append(tags, tag)
		}
	}
	if len(tags) == 0 {
		return nil, errors.New("could not extract tags from fallback response")
	}
	return tags, nil
}

func tagsFromJSON(raw json.RawMessage) []string {
	var object struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(raw, &object); err == nil && len(object.Tags) > 0 {
		return object.Tags
	}
	var array []string
	if err := json.Unmarshal(raw, &array); err == nil && len(array) > 0 {
		return array
	}
	return nil
}

func buildPrompt(input Request, knownTags []string, existingTags []string, maxTags int) string {
	var builder strings.Builder
	builder.WriteString("Analyze the meme's joke and return reusable search tags for an archive.\n")
	builder.WriteString(fmt.Sprintf("Return 2 to %d distinct tags, or fewer only when the evidence is weak.\n", maxTags))
	builder.WriteString("Read visible captions and subtitles before interpreting the visual scene. For video frames, infer the shared sequence rather than tagging each frame separately.\n")
	builder.WriteString("Choose tags for the joke's actual topic, situation, emotion, cultural reference, or recognizable format. Prefer the meaning of text or speech over literal objects.\n")
	builder.WriteString("Use short lowercase phrases. Do not include generic media descriptions, camera details, demographic guesses, or anything not supported by the supplied evidence.\n")
	builder.WriteString("Do not repeat existing tags. Existing archive tags are optional vocabulary hints, not topics you must use.\n")
	builder.WriteString(fmt.Sprintf("Filename: %s\n", strings.TrimSpace(input.Filename)))
	builder.WriteString(fmt.Sprintf("Content type: %s\n", strings.TrimSpace(input.ContentType)))
	builder.WriteString(fmt.Sprintf("Image source: %s\n", strings.TrimSpace(input.Source)))
	if len(input.AssetPaths) > 1 {
		builder.WriteString(fmt.Sprintf("Frame count: %d\n", len(input.AssetPaths)))
	}
	if transcript := strings.TrimSpace(input.Transcript); transcript != "" {
		builder.WriteString("Audio transcript (may be partial or imperfect): ")
		builder.WriteString(transcript)
		builder.WriteString("\n")
	}
	if len(existingTags) > 0 {
		builder.WriteString("Already tagged with: ")
		builder.WriteString(strings.Join(existingTags, ", "))
		builder.WriteString("\n")
	}
	if len(knownTags) > 0 {
		builder.WriteString("Optional existing tag vocabulary (reuse only when clearly relevant): ")
		builder.WriteString(strings.Join(knownTags, ", "))
		builder.WriteString("\n")
	}
	builder.WriteString("Return only a JSON object with one key named tags and an array of strings. No markdown or explanation.")
	return builder.String()
}

func normalizeTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		normalized, ok := sanitizeTag(tag)
		if !ok {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func sanitizeTag(raw string) (string, bool) {
	tag := strings.ToLower(strings.TrimSpace(raw))
	tag = strings.Trim(tag, "'\"`.,;:!? ")
	tag = strings.Join(strings.Fields(tag), " ")
	if len(tag) < 3 || len(tag) > 48 || len(strings.Fields(tag)) > 5 {
		return "", false
	}
	if strings.ContainsAny(tag, "{}[]<>\n\r\t") || tag == "tags" || strings.HasPrefix(tag, "tags:") {
		return "", false
	}
	for _, r := range tag {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || strings.ContainsRune("-'&+/", r) {
			continue
		}
		return "", false
	}
	return tag, true
}

func filterLowValueTags(tags []string, _ Request) []string {
	if len(tags) == 0 {
		return nil
	}

	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if !isArchiveUsefulTag(tag) {
			continue
		}
		filtered = append(filtered, tag)
	}
	return normalizeTags(filtered)
}

func isArchiveUsefulTag(tag string) bool {
	normalized := strings.ToLower(strings.TrimSpace(tag))
	if normalized == "" {
		return false
	}

	if len(normalized) <= 2 {
		return false
	}

	blockedExact := map[string]struct{}{
		"meme":       {},
		"funny":      {},
		"image":      {},
		"video":      {},
		"audio":      {},
		"screenshot": {},
		"picture":    {},
		"photo":      {},
		"text":       {},
		"quote":      {},
		"caption":    {},
		"subtitle":   {},
		"reaction":   {},
		"person":     {},
		"people":     {},
		"man":        {},
		"men":        {},
		"woman":      {},
		"women":      {},
		"guy":        {},
		"girl":       {},
		"face":       {},
		"camera":     {},
		"room":       {},
		"indoors":    {},
		"outdoors":   {},
		"closeup":    {},
		"vertical":   {},
		"talking":    {},
		"speaking":   {},
		"standing":   {},
		"sitting":    {},
		"smiling":    {},
		"looking":    {},
		"black":      {},
		"white":      {},
	}
	if _, blocked := blockedExact[normalized]; blocked {
		return false
	}

	return true
}
