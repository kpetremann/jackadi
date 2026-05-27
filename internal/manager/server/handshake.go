package server

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/kpetremann/jackadi/internal/manager/inventory"
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// signatureFromContext extracts metadata from gRPC context.
//
// The main information is the node ID.
func signatureFromContext(ctx context.Context, mTLSEnabled bool) (inventory.NodeIdentity, error) {
	signature := inventory.NodeIdentity{}

	nodeID, err := GetMetadataUniqueKey(ctx, "node_id")
	signature.ID = node.ID(nodeID)
	if err != nil {
		return signature, errors.New("unspecified node_id")
	}

	p, ok := peer.FromContext(ctx)
	if !ok {
		return signature, fmt.Errorf("failed to get node info")
	}

	switch addr := p.Addr.(type) {
	case *net.TCPAddr:
		signature.Address = addr.IP.String()
	case *net.UDPAddr:
		signature.Address = addr.IP.String()
	}

	if mTLSEnabled {
		tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
		if !ok {
			return signature, fmt.Errorf("unexpected node credentials")
		}
		if len(tlsInfo.State.PeerCertificates) < 1 {
			return signature, fmt.Errorf("no node certificate found")
		}

		data, err := x509.MarshalPKIXPublicKey(tlsInfo.State.PeerCertificates[0].PublicKey)
		if err != nil {
			return signature, fmt.Errorf("unable to marshal public key")
		}
		signature.Certificate = base64.StdEncoding.EncodeToString(data)
	}

	return signature, nil
}

// Handshake handles node registration.
//
// It checks if the node changed to detect potential rogue.
func (s *Server) Handshake(ctx context.Context, req *proto.HandshakeRequest) (*proto.HandshakeResponse, error) {
	resp := &proto.HandshakeResponse{Id: req.GetId()}
	nd, err := signatureFromContext(ctx, s.config.MTLSEnabled)
	if err != nil {
		return resp, status.Error(codes.InvalidArgument, err.Error())
	}

	if s.Inventory.IsRegistered(nd) {
		return resp, nil
	}

	err = s.Inventory.AddCandidate(nd)
	if err != nil {
		slog.Debug("node not added to candidates", "error", err, "node", nd.ID, "address", nd.Address)
	} else {
		slog.Debug("new node discovered", "node", nd.ID, "address", nd.Address)
	}

	if !s.config.AutoAccept {
		return resp, status.Error(codes.PermissionDenied, "node not registered")
	}

	if err := s.Inventory.Register(nd, false); err != nil {
		slog.Debug("node not auto-registered", "error", err)
		return resp, status.Error(codes.Unknown, fmt.Sprintf("failed to auto-register node: %s", err))
	}

	return resp, err
}
