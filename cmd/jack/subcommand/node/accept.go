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

func acceptCommand() *cobra.Command {
	var verbose bool
	var address, certificate string
	cmd := &cobra.Command{
		Use:   "accept NODE ...",
		Short: "accept node",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			nd := proto.NodeInfo{
				Id: args[0],
			}
			if address != "" {
				nd.Address = &address
			}
			if certificate != "" {
				nd.Certificate = &certificate
			}
			acceptedNode, err := accept(&nd)
			if err != nil {
				r := status.Convert(err)
				fmt.Fprintln(os.Stderr, r.Message())
				os.Exit(1)
			}

			style.PrettyPrint(fmt.Sprintf("node registered: %s\n", acceptedNode.String()))
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show node details")
	cmd.Flags().StringVar(&address, "address", "", "specify address of the node to accept")
	cmd.Flags().StringVar(&certificate, "certificate", "", "specify certificate of the node to accept")

	return cmd
}

func accept(nd *proto.NodeInfo) (*proto.NodeInfo, error) {
	conn, err := connection.DialCLI()
	if err != nil {
		return nil, errors.New("failed to connect the manager")
	}
	defer conn.Close()
	client := proto.NewAPIClient(conn)

	ctxReq, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	acceptedNode, err := client.AcceptNode(ctxReq, &proto.NodeRequest{Node: nd})
	if err != nil {
		return nil, err
	}

	return acceptedNode.GetNode(), nil
}
