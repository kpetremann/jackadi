// These tests are written by an AI agent
package server_test

import (
	"context"
	"testing"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/kpetremann/jackadi/internal/manager/forwarder"
	"github.com/kpetremann/jackadi/internal/manager/inventory"
	"github.com/kpetremann/jackadi/internal/manager/server"
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newHandshakeServer(t *testing.T, autoAccept bool) (*server.Server, *inventory.Nodes) {
	t.Helper()
	inv := inventory.New()
	inv.DisableRegistryFile()
	dispatcher := forwarder.NewDispatcher[*proto.TaskRequest, *proto.TaskResponse](&inv)
	opts := badger.DefaultOptions(t.TempDir()).WithLogger(nil)
	db, err := badger.Open(opts)
	if err != nil {
		t.Fatalf("failed to open badger: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	srv := server.New(server.ServerConfig{AutoAccept: autoAccept, MTLSEnabled: false}, &inv, dispatcher, db)
	return &srv, &inv
}

// handshakeCtx reuses the execStream context builder to get a properly formed incoming gRPC context.
func handshakeCtx(nodeID string) context.Context { //nolint:unparam // expected
	return newExecStream(context.Background(), nodeID).ctx
}

func TestHandshake_MissingNodeID(t *testing.T) {
	srv, _ := newHandshakeServer(t, false)

	_, err := srv.Handshake(context.Background(), &proto.HandshakeRequest{})
	if err == nil {
		t.Fatal("expected error for missing node_id metadata")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", status.Code(err))
	}
}

func TestHandshake_UnknownNode_AutoAcceptFalse(t *testing.T) {
	srv, inv := newHandshakeServer(t, false)

	_, err := srv.Handshake(handshakeCtx("node1"), &proto.HandshakeRequest{})
	if err == nil {
		t.Fatal("expected PermissionDenied for unregistered node")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", status.Code(err))
	}

	// Node should have been added as a candidate for manual review.
	_, candidates, _, _ := inv.List()
	if len(candidates) != 1 || candidates[0].ID != node.ID("node1") {
		t.Errorf("expected node1 in candidates, got %v", candidates)
	}
}

func TestHandshake_UnknownNode_AutoAcceptTrue(t *testing.T) {
	srv, inv := newHandshakeServer(t, true)

	resp, err := srv.Handshake(handshakeCtx("node1"), &proto.HandshakeRequest{Id: 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetId() != 42 {
		t.Errorf("expected Id=42, got %d", resp.GetId())
	}

	nd := inventory.NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1"}
	if !inv.IsRegistered(nd) {
		t.Error("node should be registered after auto-accept")
	}
}

func TestHandshake_AlreadyRegistered(t *testing.T) {
	srv, inv := newHandshakeServer(t, false)

	nd := inventory.NodeIdentity{ID: node.ID("node1"), Address: "127.0.0.1"}
	_ = inv.AddCandidate(nd)
	_ = inv.Register(nd, false)

	resp, err := srv.Handshake(handshakeCtx("node1"), &proto.HandshakeRequest{Id: 7})
	if err != nil {
		t.Fatalf("unexpected error for already-registered node: %v", err)
	}
	if resp.GetId() != 7 {
		t.Errorf("expected Id=7, got %d", resp.GetId())
	}

	// No new candidates should have been created.
	_, candidates, _, _ := inv.List()
	if len(candidates) != 0 {
		t.Errorf("expected no candidates, got %v", candidates)
	}
}

func TestHandshake_SameNodeSecondCall(t *testing.T) {
	srv, _ := newHandshakeServer(t, true)

	ctx := handshakeCtx("node1")

	if _, err := srv.Handshake(ctx, &proto.HandshakeRequest{}); err != nil {
		t.Fatalf("first handshake failed: %v", err)
	}
	if _, err := srv.Handshake(ctx, &proto.HandshakeRequest{}); err != nil {
		t.Fatalf("second handshake failed: %v", err)
	}
}
