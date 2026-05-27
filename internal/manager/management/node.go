package management

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/dgraph-io/badger/v4"
	"github.com/kpetremann/jackadi/internal/manager/inventory"
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ServerInterface defines the interface that the management API needs from the server.
// The goal is to ease mocking.
type ServerInterface interface {
	RequestShutdown(nodeID node.ID) error
	GetInventory() *inventory.Nodes
}

type apiServer struct {
	proto.UnimplementedAPIServer
	server ServerInterface
	db     *badger.DB
}

func New(server ServerInterface, db *badger.DB) apiServer {
	return apiServer{
		server: server,
		db:     db,
	}
}

func toProtoNodeSlice(nodes []inventory.NodeIdentity, nodesState map[node.ID]inventory.NodeState) []*proto.NodeInfo {
	resp := make([]*proto.NodeInfo, 0, len(nodes))
	for _, nd := range nodes {
		addr, cert := nd.Address, nd.Certificate
		info := proto.NodeInfo{
			Id:          string(nd.ID),
			Address:     &addr,
			Certificate: &cert,
		}

		if state, ok := nodesState[nd.ID]; ok {
			info.IsConnected = &state.Connected
			info.Since = timestamppb.New(state.Since)
			info.LastMsg = timestamppb.New(state.LastMsg)
		}

		resp = append(resp, &info)
	}
	return resp
}

// ListNodes returns the nodes the manager is seeing.
//
//   - accepted: nodes managed by the manager.
//   - candidates: nodes not yet managed by the manager, waiting to be accepted.
//   - rejected: nodes which have been explicitly excluded.
func (a *apiServer) ListNodes(ctx context.Context, req *proto.ListNodesRequest) (*proto.ListNodesResponse, error) {
	accepted, candidates, rejected, states := a.server.GetInventory().List()
	return &proto.ListNodesResponse{
		Accepted:   toProtoNodeSlice(accepted, states),
		Candidates: toProtoNodeSlice(candidates, states),
		Rejected:   toProtoNodeSlice(rejected, states),
	}, nil
}

func (a *apiServer) AcceptNode(ctx context.Context, req *proto.NodeRequest) (*proto.NodeResponse, error) {
	candidates := a.server.GetInventory().GetMatchingCandidates(node.ID(req.Node.GetId()), req.Node.Address, req.Node.Certificate)
	rejected := a.server.GetInventory().GetMatchingRejected(node.ID(req.Node.GetId()), req.Node.Address, req.Node.Certificate)

	nodes := slices.Concat(candidates, rejected)
	nd := inventory.NodeIdentity{}
	switch len(nodes) {
	case 0:
		return nil, status.Error(codes.NotFound, "node not in candidate/rejected list")
	case 1:
		nd = nodes[0]
	default:
		return nil, status.Error(codes.NotFound, "too many nodes matching")
	}

	err := a.server.GetInventory().Register(nd, true)
	if err != nil {
		slog.Debug("node not registered", "error", err)
	}

	return &proto.NodeResponse{Node: &proto.NodeInfo{
		Id:          string(nd.ID),
		Address:     &nd.Address,
		Certificate: &nd.Certificate,
	}}, err
}

// RemoveNode completely removes a node from all lists: accepted, candidates and rejected.
func (a *apiServer) RemoveNode(ctx context.Context, req *proto.NodeRequest) (*proto.NodesResponse, error) {
	accepted := a.server.GetInventory().GetMatchingAccepted(node.ID(req.Node.GetId()), req.Node.Address, req.Node.Certificate)
	candidates := a.server.GetInventory().GetMatchingCandidates(node.ID(req.Node.GetId()), req.Node.Address, req.Node.Certificate)
	rejected := a.server.GetInventory().GetMatchingRejected(node.ID(req.Node.GetId()), req.Node.Address, req.Node.Certificate)

	nodes := slices.Concat(rejected, candidates, accepted)
	if len(nodes) == 0 {
		return nil, status.Error(codes.NotFound, inventory.ErrNodeNotFound.Error())
	}

	var errs error
	removed := []inventory.NodeIdentity{}
	for _, nd := range nodes {
		if err := a.server.RequestShutdown(nd.ID); err != nil {
			slog.Info("cannot shutdown connection with node", "error", err)
		}

		if err := a.server.GetInventory().Remove(nd); err != nil {
			slog.Info("cannot unregister", "error", err)
			if joinErr := errors.Join(errs, err); joinErr != nil {
				return nil, err
			}
		}
	}

	return &proto.NodesResponse{
		Nodes: toProtoNodeSlice(removed, nil),
	}, errs
}

// RejectNode places a node in the rejected list.
//
// If the node is registered, it disconnects the node.
// A rejected node cannot register anymore unless explicitly accepted.
// It rejects only the fully matching node.
func (a *apiServer) RejectNode(ctx context.Context, req *proto.NodeRequest) (*proto.NodesResponse, error) {
	candidates := a.server.GetInventory().GetMatchingCandidates(node.ID(req.Node.GetId()), req.Node.Address, req.Node.Certificate)
	accepted := a.server.GetInventory().GetMatchingAccepted(node.ID(req.Node.GetId()), req.Node.Address, req.Node.Certificate)

	nodes := slices.Concat(candidates, accepted)
	if len(nodes) == 0 {
		return nil, status.Error(codes.NotFound, inventory.ErrNodeNotFound.Error())
	}

	var errs error
	rejected := []inventory.NodeIdentity{}
	for _, nd := range nodes {
		if a.server.GetInventory().IsRegistered(nd) {
			err := a.server.RequestShutdown(nd.ID)
			slog.Info("cannot reject because cannot shutdown connection with node", "error", err)
		}
		if err := a.server.GetInventory().Reject(nd); err != nil {
			if joinErr := errors.Join(errs, err); joinErr != nil {
				return nil, err
			}
		}
		rejected = append(rejected, nd)
	}
	return &proto.NodesResponse{
		Nodes: toProtoNodeSlice(rejected, nil),
	}, errs
}
