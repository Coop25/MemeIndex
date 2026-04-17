package client

import (
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Addr        string
	DataDir     string
	DatabaseURL string
	DiscordAuth DiscordAuthConfig
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
	Addr                   string   `envconfig:"ADDR" default:":8080"`
	DataDir                string   `envconfig:"DATA_DIR" default:"data"`
	DatabaseURL            string   `envconfig:"DATABASE_URL"`
	DiscordClientID        string   `envconfig:"DISCORD_CLIENT_ID"`
	DiscordClientSecret    string   `envconfig:"DISCORD_CLIENT_SECRET"`
	DiscordRedirectURL     string   `envconfig:"DISCORD_REDIRECT_URL"`
	DiscordDynamicRedirect bool     `envconfig:"DISCORD_DYNAMIC_REDIRECT" default:"false"`
	SessionSecret          string   `envconfig:"SESSION_SECRET"`
	SessionDurationDays    int      `envconfig:"SESSION_DURATION_DAYS" default:"30"`
	CookieSecure           bool     `envconfig:"COOKIE_SECURE" default:"false"`
	SuperAdminUserIDs      []string `envconfig:"SUPER_ADMIN_USER_IDS"`
	ViewUserIDs            []string `envconfig:"VIEW_USER_IDS"`
	AddUserIDs             []string `envconfig:"ADD_USER_IDS"`
	ManageUserIDs          []string `envconfig:"MANAGE_USER_IDS"`
}

func LoadConfig() (Config, error) {
	var raw rawConfig
	if err := envconfig.Process("memeindex", &raw); err != nil {
		return Config{}, err
	}

	return Config{
		Addr:        strings.TrimSpace(raw.Addr),
		DataDir:     strings.TrimSpace(raw.DataDir),
		DatabaseURL: strings.TrimSpace(raw.DatabaseURL),
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
