package daemon

import "os"

const cliPathPrefix = "/agyn-bin/cli"

func prependCLIPath(pathValue string) string {
	if pathValue == "" {
		return cliPathPrefix
	}
	return cliPathPrefix + string(os.PathListSeparator) + pathValue
}

func agentPathValue() string {
	return prependCLIPath(os.Getenv("PATH"))
}
