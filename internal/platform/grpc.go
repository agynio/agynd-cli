package platform

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func DialGateway(address string) (*grpc.ClientConn, error) {
	return grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
}
