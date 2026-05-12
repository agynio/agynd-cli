package daemon

import (
	"os"
	"strings"
)

const (
	cliPathPrefix    = "/agyn-bin/cli"
	agentBinPath     = "/agyn-bin"
	codexDefaultHome = "/tmp"
)

func agentPathPrefix() string {
	return cliPathPrefix + string(os.PathListSeparator) + agentBinPath
}

func prependCLIPath(pathValue string) string {
	prefix := agentPathPrefix()
	if pathValue == "" {
		return prefix
	}
	return prefix + string(os.PathListSeparator) + pathValue
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
