package daemon

import (
	"os"
	"strings"
)

const (
	agentBinPath     = "/agyn/bin"
	codexDefaultHome = "/tmp"
)

func prependCLIPath(pathValue string) string {
	if pathValue == "" {
		return agentBinPath
	}
	return agentBinPath + string(os.PathListSeparator) + pathValue
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
