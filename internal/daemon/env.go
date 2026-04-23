package daemon

import (
	"os"
	"strings"
)

const (
	cliPathPrefix     = "/agyn-bin/cli"
	codexDefaultHome  = "/tmp"
)

func prependCLIPath(pathValue string) string {
	if pathValue == "" {
		return cliPathPrefix
	}
	return cliPathPrefix + string(os.PathListSeparator) + pathValue
}

func agentPathValue() string {
	return prependCLIPath(os.Getenv("PATH"))
}

func codexHomeEnv() string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return codexDefaultHome
	}
	return home
}
