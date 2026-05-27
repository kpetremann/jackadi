package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/kpetremann/jackadi/cmd/jack/connection"
	"github.com/kpetremann/jackadi/cmd/jack/option"
	"github.com/kpetremann/jackadi/cmd/jack/style"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/kpetremann/jackadi/internal/serializer"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

func healthCommand() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "health [OPTION] ...",
		Short: "nodes health",
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := list(0)
			if err != nil {
				r := status.Convert(err)
				fmt.Fprintln(os.Stderr, r.Message())
				os.Exit(1)
			}

			if option.GetJSONFormat() {
				result, err := serializer.JSON.MarshalIndent(resp, "", "   ")
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to serialize response in JSON: %v\n", err)
					os.Exit(1)
				}
				fmt.Println(string(result))
			} else {
				in := style.Title("Nodes")
				in += prettyNodesHealthSprint(resp.Accepted, verbose)

				style.PrettyPrint(in)
			}
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show node details")

	return cmd
}

func listCommand() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "list [OPTION] ...",
		Short: "list nodes",
		Run: func(cmd *cobra.Command, args []string) {
			resp, err := list(0)
			if err != nil {
				r := status.Convert(err)
				fmt.Fprintln(os.Stderr, r.Message())
				os.Exit(1)
			}

			if option.GetJSONFormat() {
				result, err := serializer.JSON.MarshalIndent(resp, "", "   ")
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to serialize response in JSON: %v\n", err)
					os.Exit(1)
				}
				fmt.Println(string(result))
			} else {
				in := style.Title("Accepted")
				in += prettyNodeListSprint(resp.Accepted, verbose)
				in += style.Title("Candidates")
				in += prettyNodeListSprint(resp.Candidates, verbose)
				in += style.Title("Rejected")
				in += prettyNodeListSprint(resp.Rejected, verbose)

				style.PrettyPrint(in)
			}
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show node details")

	return cmd
}

func list(filter proto.Filter) (*proto.ListNodesResponse, error) {
	conn, err := connection.DialCLI()
	if err != nil {
		return nil, errors.New("failed to connect the manager")
	}
	defer conn.Close()
	client := proto.NewAPIClient(conn)

	ctxReq, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	resp, err := client.ListNodes(ctxReq, &proto.ListNodesRequest{
		Filter: filter,
	})
	if err != nil {
		return nil, errors.New("failed to get list of nodes")
	}

	return resp, err
}

func ListNode() []string {
	resp, err := list(proto.Filter_NONE)
	if err != nil {
		return nil
	}

	hosts := []string{}
	for _, n := range resp.GetAccepted() {
		hosts = append(hosts, n.GetId())
	}
	return hosts
}
