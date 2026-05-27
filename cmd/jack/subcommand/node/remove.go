package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/kpetremann/jackadi/cmd/jack/connection"
	"github.com/kpetremann/jackadi/cmd/jack/style"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

func removeCommand() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "remove NODE ...",
		Short: "remove node",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			nd := proto.NodeInfo{
				Id: args[0],
			}
			if err := remove(&nd); err != nil {
				r := status.Convert(err)
				fmt.Fprintln(os.Stderr, r.Message())
				os.Exit(1)
			}

			style.PrettyPrint(fmt.Sprintf("node removed: %s\n", nd.GetId()))
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show node details")

	return cmd
}

func remove(nd *proto.NodeInfo) error {
	conn, err := connection.DialCLI()
	if err != nil {
		return errors.New("failed to connect the manager")
	}
	defer conn.Close()
	client := proto.NewAPIClient(conn)

	ctxReq, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	_, err = client.RemoveNode(ctxReq, &proto.NodeRequest{Node: nd})
	if err != nil {
		return err
	}

	return nil
}
