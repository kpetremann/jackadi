package connection

import (
	"fmt"

	"github.com/kpetremann/jackadi/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func DialCLI() (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		fmt.Sprintf("unix:%s", config.CLISocket),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("did not connect: %w", err)
	}

	return conn, err
}
