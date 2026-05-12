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
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	ErrDisabled    = errors.New("tag suggestions are disabled")
	ErrUnavailable = errors.New("tag suggestion service is unavailable")
	ErrUnsupported = errors.New("tag suggestions are only supported for images and videos with generated thumbnails")
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
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tags": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		"required": []string{"tags"},
	}

	knownTags := append([]string(nil), input.KnownTags...)
	slices.Sort(knownTags)

	existingTags := normalizeTags(input.ExistingTags)
	prompt := buildPrompt(input, knownTags, existingTags, s.maxTags)
	body := map[string]any{
		"model":  s.model,
		"stream": false,
		"format": schema,
		"options": map[string]any{
			"temperature": 0,
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

	var parsed struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(chatResponse.Message.Content), &parsed); err != nil {
		return Result{}, fmt.Errorf("%w: invalid structured response", ErrUnavailable)
	}

	return s.buildResult(input, parsed.Tags), nil
}

func (s *Service) suggestWithGenerateFallback(ctx context.Context, input Request, imagePayloads []string, originalErr error) (Result, error) {
	prompt := buildPrompt(input, input.KnownTags, normalizeTags(input.ExistingTags), s.maxTags)
	prompt += "\nIf JSON fails, still return only a JSON object like {\"tags\":[\"tag1\",\"tag2\"]}."

	body := map[string]any{
		"model":  s.model,
		"prompt": prompt,
		"images": imagePayloads,
		"stream": false,
		"options": map[string]any{
			"temperature": 0,
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
			return Result{}, fmt.Errorf("%w: generate-only parse failed: %v", ErrUnavailable, err)
		}
		return Result{}, fmt.Errorf("%w: structured path failed (%v); fallback parse failed: %v", ErrUnavailable, originalErr, err)
	}

	return s.buildResult(input, tags), nil
}

func (s *Service) buildResult(input Request, rawTags []string) Result {
	existingTags := normalizeTags(input.ExistingTags)
	tags := normalizeTags(rawTags)
	tags = expandCompanionTags(tags)
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

	var parsed struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil && len(parsed.Tags) > 0 {
		return parsed.Tags, nil
	}

	jsonObjectPattern := regexp.MustCompile(`\{[\s\S]*\}`)
	if match := jsonObjectPattern.FindString(trimmed); strings.TrimSpace(match) != "" {
		if err := json.Unmarshal([]byte(match), &parsed); err == nil && len(parsed.Tags) > 0 {
			return parsed.Tags, nil
		}
	}

	arrayPattern := regexp.MustCompile(`\[[\s\S]*\]`)
	if match := arrayPattern.FindString(trimmed); strings.TrimSpace(match) != "" {
		var arrayValues []string
		if err := json.Unmarshal([]byte(match), &arrayValues); err == nil && len(arrayValues) > 0 {
			return arrayValues, nil
		}
	}

	lines := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '\n' || r == ',' || r == ';'
	})
	tags := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned := strings.TrimSpace(strings.TrimLeft(line, "-*0123456789. )("))
		if cleaned == "" {
			continue
		}
		tags = append(tags, cleaned)
	}
	if len(tags) == 0 {
		return nil, errors.New("could not extract tags from fallback response")
	}
	return tags, nil
}

func buildPrompt(input Request, knownTags []string, existingTags []string, maxTags int) string {
	var builder strings.Builder
	builder.WriteString("Look at this meme and suggest short reusable archive tags.\n")
	builder.WriteString(fmt.Sprintf("Return between 2 and %d tags when enough signal exists.\n", maxTags))
	builder.WriteString("Your goal is not to describe the scene. Your goal is to predict what tags a human archivist would search later to find this joke again.\n")
	builder.WriteString("Tag rules:\n")
	builder.WriteString("- Use short lowercase tags.\n")
	builder.WriteString("- First read and reason about any visible caption, subtitle, sign, or screenshot text across all provided frames.\n")
	builder.WriteString("- Think in this order: spoken/transcribed joke meaning, on-screen text meaning, then visual context.\n")
	builder.WriteString("- Prefer tags for the joke topic, political angle, social group, profession, business/work theme, emotion, reaction, or meme format.\n")
	builder.WriteString("- If the humor depends on text, prioritize text meaning over literal objects in the picture.\n")
	builder.WriteString("- Good tags answer questions like: what is this about, who is it about, what argument or stereotype is being joked about, what situation is it mocking.\n")
	builder.WriteString("- Avoid generic tags like meme, funny, image, video, screenshot, reaction, person, text, or quote.\n")
	builder.WriteString("- Avoid literal scene-description tags like woman, man, people, face, room, camera, talking, standing, sitting, black, white, indoors, outdoors, closeup, vertical, or audio unless the meme is specifically about that.\n")
	builder.WriteString("- Avoid duplicates and avoid tags already present.\n")
	builder.WriteString("- Do not invent context that is not visible.\n")
	builder.WriteString("- Do not output identity tags unless the meme text, transcript, or obvious joke context makes that identity relevant.\n")
	builder.WriteString("- When visible wording clearly names a group, ideology, job, or topic, include that direct topic as a tag.\n")
	builder.WriteString("- Prefer specific useful tags like politics, business, boss, trans, work, dating, cringe, argument, etc when justified by the meme.\n")
	builder.WriteString(fmt.Sprintf("Filename: %s\n", strings.TrimSpace(input.Filename)))
	builder.WriteString(fmt.Sprintf("Content type: %s\n", strings.TrimSpace(input.ContentType)))
	builder.WriteString(fmt.Sprintf("Image source: %s\n", strings.TrimSpace(input.Source)))
	if len(input.AssetPaths) > 1 {
		builder.WriteString(fmt.Sprintf("Frame count: %d\n", len(input.AssetPaths)))
	}
	if transcript := strings.TrimSpace(input.Transcript); transcript != "" {
		builder.WriteString("Audio transcript (may be partial): ")
		builder.WriteString(transcript)
		builder.WriteString("\n")
		builder.WriteString("- If the spoken audio changes the joke meaning, use it.\n")
	}
	if len(existingTags) > 0 {
		builder.WriteString("Already tagged with: ")
		builder.WriteString(strings.Join(existingTags, ", "))
		builder.WriteString("\n")
	}
	if len(knownTags) > 0 {
		builder.WriteString("Prefer reusing these existing archive tags when they fit: ")
		builder.WriteString(strings.Join(knownTags, ", "))
		builder.WriteString("\n")
	}
	builder.WriteString("Only output tags that would still make sense without seeing the image, because they describe the meme's idea rather than its raw pixels.\n")
	builder.WriteString("Respond only with JSON matching the provided schema.")
	return builder.String()
}

func normalizeTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		if normalized == "" {
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

func expandCompanionTags(tags []string) []string {
	expanded := append([]string(nil), tags...)

	if hasAnyTag(tags,
		"trans",
		"transgender",
		"gender",
		"pronouns",
		"feminism",
		"equality",
		"activism",
		"woke",
		"dei",
		"diversity",
		"inclusion",
		"lgbt",
		"gay",
		"lesbian",
		"bisexual",
		"queer",
		"rights",
		"social-justice",
		"social justice",
		"identity",
		"ideology",
	) {
		expanded = append(expanded, "politics")
	}

	return normalizeTags(expanded)
}

func filterLowValueTags(tags []string, input Request) []string {
	if len(tags) == 0 {
		return nil
	}

	contextText := strings.ToLower(strings.TrimSpace(input.Filename + " " + input.Transcript))
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if !isArchiveUsefulTag(tag, contextText) {
			continue
		}
		filtered = append(filtered, tag)
	}
	return normalizeTags(filtered)
}

func isArchiveUsefulTag(tag string, contextText string) bool {
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

	if strings.Contains(normalized, "frame") || strings.Contains(normalized, "image") || strings.Contains(normalized, "video") {
		return false
	}

	identityTags := map[string]struct{}{
		"male":         {},
		"female":       {},
		"gender":       {},
		"trans":        {},
		"transgender":  {},
		"gay":          {},
		"lesbian":      {},
		"queer":        {},
		"lgbt":         {},
		"race":         {},
		"racial":       {},
		"black-people": {},
		"white-people": {},
	}
	if _, isIdentity := identityTags[normalized]; isIdentity {
		return contextText != "" && strings.Contains(contextText, strings.ReplaceAll(normalized, "-", " "))
	}

	return true
}

func hasAnyTag(tags []string, needles ...string) bool {
	if len(tags) == 0 || len(needles) == 0 {
		return false
	}

	for _, tag := range tags {
		normalizedTag := strings.ToLower(strings.TrimSpace(tag))
		for _, needle := range needles {
			normalizedNeedle := strings.ToLower(strings.TrimSpace(needle))
			if normalizedNeedle == "" {
				continue
			}
			if normalizedTag == normalizedNeedle {
				return true
			}
		}
	}

	return false
}
