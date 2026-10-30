package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
)

var holderChdir = os.Chdir

func runHolder(ctx context.Context, wake <-chan os.Signal, client initScriptsClient, environmentID string, workDir string) error {
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
	for {
		select {
		case <-ctx.Done():
			return operationError(opProcessSignalShutdown, 0, ctx.Err())
		case <-wake:
			// A sandbox holds no subscription and runs no turn, and its gateway
			// connection reopens itself on the next call: the wake is noted and
			// nothing else.
			log.Printf("wake signal received; holder has nothing to resubscribe")
		}
	}
}
