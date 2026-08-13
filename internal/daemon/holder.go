package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
)

var holderChdir = os.Chdir

func runHolder(ctx context.Context, client initScriptsClient, environmentID string, workDir string) error {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return fmt.Errorf("holder work dir is required")
	}
	if err := holderChdir(workDir); err != nil {
		return fmt.Errorf("set holder work dir %s: %w", workDir, err)
	}
	log.Printf("agynd holder mode started in %s", workDir)
	// Only the environment's: a sandbox has no agent, so there are no
	// agent-scoped scripts to follow them.
	if environmentID = strings.TrimSpace(environmentID); environmentID != "" {
		if err := runInitScripts(ctx, client, "", environmentID, workDir); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return operationError(opProcessSignalShutdown, 0, ctx.Err())
}
