package client

import "strings"

var buildVersion = "dev"

func BuildVersion() string {
	version := strings.TrimSpace(buildVersion)
	if version == "" {
		return "dev"
	}
	return version
}
