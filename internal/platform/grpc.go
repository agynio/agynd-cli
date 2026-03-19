package platform

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Dial(ctx context.Context, address string) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, address, grpc.WithTransportCredentials(insecure.NewCredentials()))
}
