package node

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kpetremann/jackadi/cmd/jack/option"
	"github.com/kpetremann/jackadi/cmd/jack/style"
	"github.com/kpetremann/jackadi/internal/proto"
)

func sortNodeFunc(a *proto.NodeInfo, b *proto.NodeInfo) int {
	if a == nil || b == nil {
		return 0
	}
	return strings.Compare(a.Id, b.Id)
}

func prettyNodeListSprint(nodes []*proto.NodeInfo, showDetails bool) string {
	var items strings.Builder
	if option.GetSortOutput() {
		slices.SortFunc(nodes, sortNodeFunc)
	}

	for _, nd := range nodes {
		if !showDetails {
			items.WriteString(style.Item(nd.GetId()))
			continue
		}
		if nd.GetCertificate() != "" {
			items.WriteString(style.Item(fmt.Sprintf("%s (%s %s)", nd.GetId(), nd.GetAddress(), nd.GetCertificate())))
		} else {
			items.WriteString(style.Item(fmt.Sprintf("%s (%s)", nd.GetId(), nd.GetAddress())))
		}
	}

	return style.SpacedBlock(items.String())
}

func prettyTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("January 2, 2006 at 15:04 UTC")
}

func prettyNodesHealthSprint(nodes []*proto.NodeInfo, showDetails bool) string {
	var items strings.Builder
	if option.GetSortOutput() {
		slices.SortFunc(nodes, sortNodeFunc)
	}

	for _, nd := range nodes {
		connectedState := "connected"

		if !nd.GetIsConnected() {
			connectedState = "disconnected"
		}

		lastActive := nd.GetLastMsg().AsTime()

		if !showDetails {
			items.WriteString(style.Item(fmt.Sprintf("%s: %s", nd.GetId(), connectedState)))
			continue
		}

		items.WriteString(style.Item(nd.GetId()))
		items.WriteString(style.SubItem(fmt.Sprintf("state: %s", connectedState)))
		items.WriteString(style.SubItem(fmt.Sprintf("%s since: %s", connectedState, prettyTime(nd.GetSince().AsTime()))))
		items.WriteString(style.SubItem(fmt.Sprintf("last event: %s", prettyTime(lastActive))))
	}

	return style.SpacedBlock(items.String())
}
