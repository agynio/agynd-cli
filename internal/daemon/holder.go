package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
)

var holderChdir = os.Chdir

func runHolder(ctx context.Context, workDir string) error {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return fmt.Errorf("holder work dir is required")
	}
	if err := holderChdir(workDir); err != nil {
		return fmt.Errorf("set holder work dir %s: %w", workDir, err)
	}
	// Holder spawns nothing, but it is not inert: the file has to exist before
	// an engineer opens a shell, which is the only session this mode serves.
	if err := writePlaceholderFile(); err != nil {
		return fmt.Errorf("write placeholder credential: %w", err)
	}
	log.Printf("agynd holder mode started in %s", workDir)
	<-ctx.Done()
	return operationError(opProcessSignalShutdown, 0, ctx.Err())
}
