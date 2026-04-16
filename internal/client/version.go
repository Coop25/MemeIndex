package client

import (
	"os"
	"strings"
)

var buildVersion = "dev"

func BuildVersion() string {
	version := strings.TrimSpace(buildVersion)
	if version != "" && version != "dev" {
		return version
	}

	for _, key := range []string{"MEMEINDEX_VERSION", "APP_VERSION"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}

	if version == "" {
		return "dev"
	}
	return version
}
