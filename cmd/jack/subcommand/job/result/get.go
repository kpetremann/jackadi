package result

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kpetremann/jackadi/cmd/jack/connection"
	"github.com/kpetremann/jackadi/cmd/jack/style"
	"github.com/kpetremann/jackadi/internal/manager/database"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

func getCommand() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "get ID ...",
		Short: "get results from ID",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			conn, err := connection.DialCLI()
			if err != nil {
				fmt.Fprintln(os.Stderr, "failed to connect the manager")
				os.Exit(1)
			}
			defer conn.Close()
			client := proto.NewAPIClient(conn)

			res, err := getResult(client, args[0])
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			out := getRequest(client, args)

			style.PrettyPrint(out + "\n" + res)
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show node details")

	return cmd
}

func fetchResult(client proto.APIClient, id string) (*proto.ResultsResponse, error) {
	ctxReq, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	return client.GetResults(ctxReq, &proto.ResultsRequest{ResultID: id})
}

func getResult(client proto.APIClient, id string) (string, error) {
	r, err := fetchResult(client, id)
	if err != nil {
		r := status.Convert(err)
		fmt.Println(r.Message())
		return "", err
	}

	if value, changed := database.CutGroupPrefix(r.GetResult()); changed {
		ids := strings.Split(value, ",")
		var out strings.Builder
		for _, subid := range ids {
			if id == subid {
				return "", errors.New("groupID included in group: stopping infinite loop")
			}
			p, err := getResult(client, subid)
			if err != nil {
				return "", err
			}
			out.WriteString(p)
		}

		return out.String(), nil
	}

	return prettySprint([]byte(r.GetResult()))
}

func fetchRequest(client proto.APIClient, requestID string) (string, error) {
	ctxReq, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	resp, err := client.GetRequest(ctxReq, &proto.RequestRequest{RequestID: requestID})
	if err != nil {
		return "", err
	}

	dbRequest, err := database.UnmarshalRequest([]byte(resp.GetRequest()))
	if err != nil {
		return "", err
	}

	out := ""
	if dbRequest.Task != "" {
		out += style.InlineBlockTitle("Task") + dbRequest.Task
	}
	if len(dbRequest.ConnectedTarget) > 0 {
		out += style.InlineBlockTitle("Connected targets") + strings.Join(dbRequest.ConnectedTarget, ", ")
	}
	if len(dbRequest.DisconnectedTarget) > 0 {
		out += style.InlineBlockTitle("Disconnected targets") + strings.Join(dbRequest.DisconnectedTarget, ", ")
	}

	return out, nil
}

func fetchRequestID(client proto.APIClient, resultID string) (string, error) {
	r, err := fetchResult(client, resultID)
	if err != nil {
		return "", err
	}

	requestID, err := database.ExtractRequestIDFromResult(r.GetResult(), resultID)
	if err != nil {
		return "", err
	}

	// Remove the request prefix since the caller expects just the ID
	prefix := fmt.Sprintf("%s:", database.RequestKeyPrefix)
	if after, ok := strings.CutPrefix(requestID, prefix); ok {
		return after, nil
	}

	return requestID, nil
}

func getRequest(client proto.APIClient, args []string) string {
	out := style.Title("Request:")
	requestID, err := fetchRequestID(client, args[0])
	if err != nil || requestID == "" {
		out += style.RenderError(fmt.Sprintf("Warning: Could not determine request ID: %v\n", err))
		return out
	}

	req, err := fetchRequest(client, requestID)
	if err != nil {
		out += style.RenderError(fmt.Sprintf("Warning: Could not fetch request %s: %v\n", requestID, err))
		return out
	}

	return out + req
}
