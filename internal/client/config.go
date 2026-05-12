package client

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Addr                  string
	DataDir               string
	DatabaseURL           string
	MediaFetchYTDLPBinary string
	MediaFetchRetry       MediaFetchRetryConfig
	TagSuggestions        TagSuggestionsConfig
	DiscordAuth           DiscordAuthConfig
}

type MediaFetchRetryConfig struct {
	Interval    time.Duration
	MaxAttempts int
}

type TagSuggestionsConfig struct {
	OllamaURL         string
	Model             string
	Timeout           time.Duration
	MaxTags           int
	KnownTagBudget    int
	FastMode          bool
	GenerateOnly      bool
	VideoFrameCount   int
	VideoFrameWidth   int
	DisableTranscript bool
	TranscribeBinary  string
	TranscribeArgs    []string
	TranscribeTimeout time.Duration
}

func (c TagSuggestionsConfig) Enabled() bool {
	return c.OllamaURL != "" && c.Model != ""
}

type DiscordAuthConfig struct {
	ClientID          string
	ClientSecret      string
	RedirectURL       string
	DynamicRedirect   bool
	SessionSecret     string
	SessionDuration   time.Duration
	CookieSecure      bool
	SuperAdminUserIDs map[string]struct{}
	ViewUserIDs       map[string]struct{}
	AddUserIDs        map[string]struct{}
}

func (c DiscordAuthConfig) Enabled() bool {
	return c.ClientID != "" && c.ClientSecret != "" && (c.RedirectURL != "" || c.DynamicRedirect) && c.SessionSecret != ""
}

type rawConfig struct {
	Addr                            string   `envconfig:"ADDR" default:":8080"`
	DataDir                         string   `envconfig:"DATA_DIR" default:"data"`
	DatabaseURL                     string   `envconfig:"DATABASE_URL"`
	MediaFetchYTDLPBinary           string   `envconfig:"MEDIAFETCH_YTDLP_BINARY" default:"yt-dlp"`
	MediaFetchRetryIntervalSecs     int      `envconfig:"MEDIAFETCH_RETRY_INTERVAL_SECONDS" default:"300"`
	MediaFetchRetryMaxAttempts      int      `envconfig:"MEDIAFETCH_RETRY_MAX_ATTEMPTS" default:"3"`
	TagSuggestOllamaURL             string   `envconfig:"TAGSUGGEST_OLLAMA_URL"`
	TagSuggestOllamaModel           string   `envconfig:"TAGSUGGEST_OLLAMA_MODEL"`
	TagSuggestTimeoutSecs           int      `envconfig:"TAGSUGGEST_TIMEOUT_SECONDS" default:"300"`
	TagSuggestMaxTags               int      `envconfig:"TAGSUGGEST_MAX_TAGS" default:"8"`
	TagSuggestKnownTagHint          int      `envconfig:"TAGSUGGEST_KNOWN_TAG_BUDGET" default:"150"`
	TagSuggestFastMode              bool     `envconfig:"TAGSUGGEST_FAST_MODE" default:"false"`
	TagSuggestGenerateOnly          bool     `envconfig:"TAGSUGGEST_GENERATE_ONLY" default:"false"`
	TagSuggestVideoFrameCount       string   `envconfig:"TAGSUGGEST_VIDEO_FRAME_COUNT"`
	TagSuggestVideoFrameWidth       string   `envconfig:"TAGSUGGEST_VIDEO_FRAME_WIDTH"`
	TagSuggestDisableTranscript     bool     `envconfig:"TAGSUGGEST_DISABLE_TRANSCRIPTION" default:"false"`
	TagSuggestTranscribeBinary      string   `envconfig:"TAGSUGGEST_TRANSCRIBE_BINARY"`
	TagSuggestTranscribeArgs        []string `envconfig:"TAGSUGGEST_TRANSCRIBE_ARGS"`
	TagSuggestTranscribeTimeoutSecs int      `envconfig:"TAGSUGGEST_TRANSCRIBE_TIMEOUT_SECONDS" default:"120"`
	DiscordClientID                 string   `envconfig:"DISCORD_CLIENT_ID"`
	DiscordClientSecret             string   `envconfig:"DISCORD_CLIENT_SECRET"`
	DiscordRedirectURL              string   `envconfig:"DISCORD_REDIRECT_URL"`
	DiscordDynamicRedirect          bool     `envconfig:"DISCORD_DYNAMIC_REDIRECT" default:"false"`
	SessionSecret                   string   `envconfig:"SESSION_SECRET"`
	SessionDurationDays             int      `envconfig:"SESSION_DURATION_DAYS" default:"30"`
	CookieSecure                    bool     `envconfig:"COOKIE_SECURE" default:"false"`
	SuperAdminUserIDs               []string `envconfig:"SUPER_ADMIN_USER_IDS"`
	ViewUserIDs                     []string `envconfig:"VIEW_USER_IDS"`
	AddUserIDs                      []string `envconfig:"ADD_USER_IDS"`
	ManageUserIDs                   []string `envconfig:"MANAGE_USER_IDS"`
}

func LoadConfig() (Config, error) {
	var raw rawConfig
	if err := envconfig.Process("memeindex", &raw); err != nil {
		return Config{}, err
	}

	frameCount, err := parseOptionalPositiveInt(raw.TagSuggestVideoFrameCount)
	if err != nil {
		return Config{}, fmt.Errorf("invalid MEMEINDEX_TAGSUGGEST_VIDEO_FRAME_COUNT: %w", err)
	}
	if frameCount <= 0 {
		if raw.TagSuggestFastMode {
			frameCount = 1
		} else {
			frameCount = 3
		}
	}
	frameWidth, err := parseOptionalPositiveInt(raw.TagSuggestVideoFrameWidth)
	if err != nil {
		return Config{}, fmt.Errorf("invalid MEMEINDEX_TAGSUGGEST_VIDEO_FRAME_WIDTH: %w", err)
	}
	if frameWidth <= 0 {
		if raw.TagSuggestFastMode {
			frameWidth = 320
		} else {
			frameWidth = 480
		}
	}
	disableTranscript := raw.TagSuggestDisableTranscript || raw.TagSuggestFastMode
	modelName := strings.TrimSpace(raw.TagSuggestOllamaModel)
	generateOnly := raw.TagSuggestGenerateOnly || shouldPreferGenerateOnlyModel(modelName)

	return Config{
		Addr:                  strings.TrimSpace(raw.Addr),
		DataDir:               strings.TrimSpace(raw.DataDir),
		DatabaseURL:           strings.TrimSpace(raw.DatabaseURL),
		MediaFetchYTDLPBinary: strings.TrimSpace(raw.MediaFetchYTDLPBinary),
		MediaFetchRetry: MediaFetchRetryConfig{
			Interval:    time.Duration(max(raw.MediaFetchRetryIntervalSecs, 1)) * time.Second,
			MaxAttempts: max(raw.MediaFetchRetryMaxAttempts, 1),
		},
		TagSuggestions: TagSuggestionsConfig{
			OllamaURL:         strings.TrimSpace(raw.TagSuggestOllamaURL),
			Model:             modelName,
			Timeout:           time.Duration(max(raw.TagSuggestTimeoutSecs, 1)) * time.Second,
			MaxTags:           max(raw.TagSuggestMaxTags, 1),
			KnownTagBudget:    max(raw.TagSuggestKnownTagHint, 1),
			FastMode:          raw.TagSuggestFastMode,
			GenerateOnly:      generateOnly,
			VideoFrameCount:   max(frameCount, 1),
			VideoFrameWidth:   max(frameWidth, 160),
			DisableTranscript: disableTranscript,
			TranscribeBinary:  strings.TrimSpace(raw.TagSuggestTranscribeBinary),
			TranscribeArgs:    append([]string(nil), raw.TagSuggestTranscribeArgs...),
			TranscribeTimeout: time.Duration(max(raw.TagSuggestTranscribeTimeoutSecs, 1)) * time.Second,
		},
		DiscordAuth: DiscordAuthConfig{
			ClientID:        strings.TrimSpace(raw.DiscordClientID),
			ClientSecret:    strings.TrimSpace(raw.DiscordClientSecret),
			RedirectURL:     strings.TrimSpace(raw.DiscordRedirectURL),
			DynamicRedirect: raw.DiscordDynamicRedirect,
			SessionSecret:   strings.TrimSpace(raw.SessionSecret),
			SessionDuration: sessionDurationFromDays(raw.SessionDurationDays),
			CookieSecure:    raw.CookieSecure,
			SuperAdminUserIDs: mergeSets(
				toSet(raw.SuperAdminUserIDs),
				toSet(raw.ManageUserIDs),
			),
			ViewUserIDs: toSet(raw.ViewUserIDs),
			AddUserIDs:  toSet(raw.AddUserIDs),
		},
	}, nil
}

func toSet(rawValues []string) map[string]struct{} {
	values := map[string]struct{}{}
	for _, part := range rawValues {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		values[value] = struct{}{}
	}
	return values
}

func sessionDurationFromDays(days int) time.Duration {
	if days <= 0 {
		days = 30
	}
	return time.Duration(days) * 24 * time.Hour
}

func mergeSets(sets ...map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for _, set := range sets {
		for key := range set {
			out[key] = struct{}{}
		}
	}
	return out
}

func max(value int, fallback int) int {
	if value < fallback {
		return fallback
	}
	return value
}

func parseOptionalPositiveInt(rawValue string) (int, error) {
	value := strings.TrimSpace(rawValue)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func shouldPreferGenerateOnlyModel(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(normalized, "qwen2.5vl")
}
