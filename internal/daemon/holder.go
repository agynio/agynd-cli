package daemon

import (
	"context"
	"log"
)

func runHolder(ctx context.Context) error {
	log.Printf("agynd holder mode started")
	<-ctx.Done()
	return operationError(opProcessSignalShutdown, 0, ctx.Err())
}
