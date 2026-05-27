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
	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/spf13/cobra"
)

func formatResultItem(id int64, date, nodes, status string) string {
	idStr := fmt.Sprintf("%d", id)
	statusSymbol := "✓"

	if strings.Contains(status, "error") {
		statusSymbol = "✗"
	} else if status == "unknown" {
		statusSymbol = "?"
	}

	idColored := style.RenderID(idStr)
	var statusSymbolColored string
	switch {
	case strings.Contains(status, "error"):
		statusSymbolColored = style.RenderError(statusSymbol)
	case status == "unknown":
		statusSymbolColored = style.RenderUnknown(statusSymbol)
	default:
		statusSymbolColored = style.RenderSuccess(statusSymbol)
	}

	return fmt.Sprintf("[%s] %s - %s\n    %s\n\n", statusSymbolColored, idColored, date, nodes)
}

func listCommand() *cobra.Command {
	limit := int32(config.ResultsLimit)
	offset := int32(0)
	fromDate := int64(0)
	toDate := int64(0)
	targets := []string{}
	fromStr := ""
	toStr := ""

	cmd := &cobra.Command{
		Use:   "list",
		Short: "list results",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			// Validate limit does not exceed maximum
			if limit > config.ResultsPageLimit {
				limit = config.ResultsPageLimit
				fmt.Printf("Warning: limit exceeded maximum (%d), using maximum value\n", config.ResultsPageLimit)
			}

			// Parse date strings if provided
			if fromStr != "" {
				t, err := parseTimeString(fromStr)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Invalid from-date format: %s\n", err)
					os.Exit(1)
				}
				fromDate = t.UnixNano()
			}

			if toStr != "" {
				t, err := parseTimeString(toStr)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Invalid to-date format: %s\n", err)
					os.Exit(1)
				}
				toDate = t.UnixNano()
			}

			if fromStr != "" && toStr != "" && toDate < fromDate {
				fmt.Fprintf(os.Stderr, "'to' date must be after 'from' date\n")
				os.Exit(1)
			}

			res, err := list(limit, offset, fromDate, toDate, targets)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			style.PrettyPrint(res)
		},
	}
	cmd.Flags().Int32VarP(&limit, "limit", "l", 100, fmt.Sprintf("maximum number of results to return (max: %d)", config.ResultsPageLimit))
	cmd.Flags().Int32VarP(&offset, "offset", "o", 0, "starting position for pagination")
	cmd.Flags().StringVar(&fromStr, "from", "", "filter results from this date (format: 2006-01-01 or 2006-01-01 15:04:05)")
	cmd.Flags().StringVar(&toStr, "to", "", "filter results up to this date (format: 2006-01-01 or 2006-01-01 15:04:05)")
	cmd.Flags().StringSliceVarP(&targets, "targets", "t", []string{}, "filter results by node IDs (comma separated)")

	return cmd
}

// parseTimeString parses time strings in formats like "2025-01-01" or "2025-01-01 15:04:05".
func parseTimeString(timeStr string) (time.Time, error) {
	layouts := []string{
		"2006-01-02",
		"2006-01-02 15:04:05",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, timeStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported time format: %s", timeStr)
}

func list(limit, offset int32, fromDate, toDate int64, targets []string) (string, error) {
	conn, err := connection.DialCLI()
	if err != nil {
		return "", errors.New("failed to connect the manager")
	}
	defer conn.Close()
	client := proto.NewAPIClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	req := &proto.ListResultsRequest{
		Limit:    limit,
		Offset:   offset,
		FromDate: &fromDate,
		ToDate:   &toDate,
		Targets:  targets,
	}

	resp, err := client.ListResults(ctx, req)
	if err != nil {
		return "", err
	}

	out := style.Title("Task results")

	// Handle filters
	filters := []string{}
	if fromDate > 0 {
		t := time.Unix(0, fromDate)
		filters = append(filters, fmt.Sprintf("from: %s", t.Format("2006-01-02 15:04:05")))
	}

	if toDate > 0 {
		t := time.Unix(0, toDate)
		filters = append(filters, fmt.Sprintf("to: %s", t.Format("2006-01-02 15:04:05")))
	}

	if len(targets) > 0 {
		filters = append(filters, fmt.Sprintf("targets: %s", strings.Join(targets, ", ")))
	}

	if offset > 0 {
		filters = append(filters, fmt.Sprintf("offset: %d", offset))
	}

	if len(filters) > 0 {
		out += style.Subtitle(fmt.Sprintf("Filters: %s", strings.Join(filters, ", ")))
	}

	results := resp.GetResults()
	if len(results) == 0 {
		out += style.SpacedBlock(style.Item("No results found"))
		return out, nil
	}

	var items strings.Builder
	for _, result := range results {
		id := result.GetId()
		timestamp := time.Unix(0, id)
		date := timestamp.Format("2006-01-02 15:04:05")

		var targets string
		if nodeTarget := result.GetNode(); strings.HasPrefix(nodeTarget, "grouped:") {
			// Grouped results contains only a list of sub IDs as target (not nodes directly).
			// `jack results get <groupedID>` will fetch all the results for all sub IDs
			targets, _ = strings.CutPrefix(nodeTarget, "grouped:")
		} else {
			// Only one node targeted in a normal result
			targets = result.GetNode()
		}

		status := result.GetStatus()
		items.WriteString(formatResultItem(id, date, targets, status))
	}

	paginationInfo := fmt.Sprintf("Showing %d results (limit: %d, offset: %d)", len(results), limit, offset)

	return fmt.Sprintf("%s\n%s%s", out, items.String(), style.Subtitle(paginationInfo)), nil
}
