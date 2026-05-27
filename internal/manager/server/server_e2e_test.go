// These tests are written by an AI agent
package server_test

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/kpetremann/jackadi/internal/manager/forwarder"
	"github.com/kpetremann/jackadi/internal/manager/inventory"
	"github.com/kpetremann/jackadi/internal/manager/server"
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// execStream is a mock bidirectional gRPC stream connecting the manager server to a simulated node.
//
// From the server's perspective (Cluster_ExecTaskServer):
//   - Send(*TaskRequest)        → writes a task request into toNode
//   - Recv() (*TaskResponse, _) → reads a task response from fromNode
//
// The test simulates the node by reading from toNode and writing to fromNode.
type execStream struct {
	ctx      context.Context
	cancel   context.CancelFunc
	toNode   chan *proto.TaskRequest  // server → node
	fromNode chan *proto.TaskResponse // node → server
	mu       sync.Mutex
	closed   bool
}

func newExecStream(parent context.Context, nodeID string) *execStream {
	md := metadata.Pairs("node_id", nodeID)
	ctx := metadata.NewIncomingContext(parent, md)
	ctx = peer.NewContext(ctx, &peer.Peer{
		Addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9999},
	})
	ctx, cancel := context.WithCancel(ctx)
	return &execStream{
		ctx:      ctx,
		cancel:   cancel,
		toNode:   make(chan *proto.TaskRequest, 10),
		fromNode: make(chan *proto.TaskResponse, 10),
	}
}

// server-side stream interface

func (s *execStream) Send(req *proto.TaskRequest) error {
	select {
	case s.toNode <- req:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *execStream) Recv() (*proto.TaskResponse, error) {
	select {
	case resp, ok := <-s.fromNode:
		if !ok {
			return nil, io.EOF
		}
		return resp, nil
	case <-s.ctx.Done():
		return nil, io.EOF
	}
}

func (s *execStream) Context() context.Context { return s.ctx }

// grpc.ServerStream stubs (unused in these tests).
func (s *execStream) SetHeader(metadata.MD) error  { return nil }
func (s *execStream) SendHeader(metadata.MD) error { return nil }
func (s *execStream) SetTrailer(metadata.MD)       {}
func (s *execStream) SendMsg(any) error            { return nil }
func (s *execStream) RecvMsg(any) error            { return nil }

// node-facing helpers

// nodeRecv reads the next request sent by the server to the simulated node.
func (s *execStream) nodeRecv(timeout time.Duration) (*proto.TaskRequest, error) {
	select {
	case req, ok := <-s.toNode:
		if !ok {
			return nil, io.EOF
		}
		return req, nil
	case <-time.After(timeout):
		return nil, io.EOF
	}
}

// nodeReply sends a successful task response back to the server.
func (s *execStream) nodeReply(req *proto.TaskRequest, output []byte) {
	s.fromNode <- &proto.TaskResponse{
		Id:            req.GetId(),
		GroupID:       req.GroupID,
		InternalError: proto.InternalError_OK,
		Output:        output,
	}
}

// nodeDisconnect simulates the node dropping the connection (closes the response channel).
func (s *execStream) nodeDisconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.fromNode)
	}
}

// harness wires up all components for end-to-end testing without a real network.
type harness struct {
	inv        *inventory.Nodes
	dispatcher forwarder.Dispatcher[*proto.TaskRequest, *proto.TaskResponse]
	srv        *server.Server
	fwd        *forwarder.GRPCForwarder
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	inv := inventory.New()
	inv.DisableRegistryFile()

	dispatcher := forwarder.NewDispatcher[*proto.TaskRequest, *proto.TaskResponse](&inv)

	opts := badger.DefaultOptions(t.TempDir()).WithLogger(nil)
	db, err := badger.Open(opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	srv := server.New(server.ServerConfig{AutoAccept: false, MTLSEnabled: false}, &inv, dispatcher, db)
	fwd := forwarder.New(dispatcher, db)

	return &harness{
		inv:        &inv,
		dispatcher: dispatcher,
		srv:        &srv,
		fwd:        &fwd,
	}
}

// connectNode pre-registers a node in the inventory and starts the server's ExecTask handler.
// Returns the stream (for the test to drive node behaviour) and an error channel that receives
// the return value of ExecTask when the stream ends.
func (h *harness) connectNode(t *testing.T, nodeID string) (*execStream, chan error) {
	t.Helper()
	nd := inventory.NodeIdentity{ID: node.ID(nodeID), Address: "127.0.0.1"}
	require.NoError(t, h.inv.AddCandidate(nd))
	require.NoError(t, h.inv.Register(nd, false))

	stream := newExecStream(context.Background(), nodeID)
	errCh := make(chan error, 1)
	go func() { errCh <- h.srv.ExecTask(stream) }()

	require.Eventually(t, func() bool {
		nodes, err := h.dispatcher.TargetedNodes(nodeID, proto.TargetMode_EXACT)
		return err == nil && nodes[nodeID]
	}, 2*time.Second, 10*time.Millisecond, "node %q never connected to dispatcher", nodeID)

	return stream, errCh
}

// execTask forwards a task to the given node and returns the forwarder's response.
func (h *harness) execTask(ctx context.Context, nodeID, task string, timeoutSec uint32) (*proto.FwdResponse, error) { //nolint:unparam // expected
	return h.fwd.ExecTask(ctx, &proto.TaskRequest{
		Target:     nodeID,
		TargetMode: proto.TargetMode_EXACT,
		Task:       task,
		Timeout:    timeoutSec,
	})
}

// --- Tests ---

// TestE2E_AllOK verifies the happy path: forwarder sends a task, the node processes it,
// and the response is correctly returned to the caller.
func TestE2E_AllOK(t *testing.T) {
	h := newHarness(t)
	stream, srvErrCh := h.connectNode(t, "node1")

	// Simulate node: receive the task and reply with a successful response.
	go func() {
		req, err := stream.nodeRecv(2 * time.Second)
		if err != nil {
			return
		}
		stream.nodeReply(req, []byte(`"hello"`))
	}()

	resp, err := h.execTask(context.Background(), "node1", "cmd.run", 5)
	require.NoError(t, err)

	nodeResp := resp.GetResponses()["node1"]
	require.NotNil(t, nodeResp)
	assert.Equal(t, proto.InternalError_OK, nodeResp.GetInternalError())
	assert.Equal(t, []byte(`"hello"`), nodeResp.GetOutput())

	stream.cancel()
	<-srvErrCh
}

// TestE2E_NodeDisconnected verifies that when the target node is not connected,
// the forwarder immediately returns DISCONNECTED without blocking.
func TestE2E_NodeDisconnected(t *testing.T) {
	h := newHarness(t)
	// No node is connected: targeting it returns disconnected status.

	resp, err := h.execTask(context.Background(), "node1", "cmd.run", 5)
	require.NoError(t, err)

	nodeResp := resp.GetResponses()["node1"]
	require.NotNil(t, nodeResp)
	assert.Equal(t, proto.InternalError_DISCONNECTED, nodeResp.GetInternalError())
}

// TestE2E_NodeDisconnectsDuringExec verifies that when a node drops its connection
// after receiving a task but before responding, the forwarder eventually times out.
func TestE2E_NodeDisconnectsDuringExec(t *testing.T) {
	h := newHarness(t)
	stream, srvErrCh := h.connectNode(t, "node1")

	resultCh := make(chan *proto.FwdResponse, 1)
	go func() {
		resp, _ := h.execTask(context.Background(), "node1", "cmd.run", 2) // 2s timeout
		resultCh <- resp
	}()

	// Wait for the task to arrive at the node, then disconnect without responding.
	req, err := stream.nodeRecv(2 * time.Second)
	require.NoError(t, err, "task never reached the node")
	_ = req

	stream.nodeDisconnect()

	// The forwarder has no response channel to read from — it must wait for the task timeout.
	select {
	case resp := <-resultCh:
		nodeResp := resp.GetResponses()["node1"]
		require.NotNil(t, nodeResp)
		assert.Equal(t, proto.InternalError_TIMEOUT, nodeResp.GetInternalError())
	case <-time.After(5 * time.Second):
		t.Fatal("forwarder did not return after node disconnect + task timeout")
	}

	<-srvErrCh
}

// reconnectNode starts a new ExecTask stream for a node that is already registered in the
// inventory (e.g. after a previous disconnect). Unlike connectNode it skips inventory setup.
func (h *harness) reconnectNode(t *testing.T, nodeID string) (*execStream, chan error) {
	t.Helper()
	stream := newExecStream(context.Background(), nodeID)
	errCh := make(chan error, 1)
	go func() { errCh <- h.srv.ExecTask(stream) }()

	require.Eventually(t, func() bool {
		nodes, err := h.dispatcher.TargetedNodes(nodeID, proto.TargetMode_EXACT)
		return err == nil && nodes[nodeID]
	}, 2*time.Second, 10*time.Millisecond, "node %q never reconnected", nodeID)

	return stream, errCh
}

// TestE2E_MultinodeAllOK verifies that a task broadcast to multiple nodes returns a
// successful response from each.
func TestE2E_MultinodeAllOK(t *testing.T) {
	h := newHarness(t)
	stream1, srvErrCh1 := h.connectNode(t, "node1")
	stream2, srvErrCh2 := h.connectNode(t, "node2")

	for _, s := range []*execStream{stream1, stream2} {
		go func() {
			req, err := s.nodeRecv(2 * time.Second)
			if err != nil {
				return
			}
			s.nodeReply(req, []byte(`"ok"`))
		}()
	}

	resp, err := h.fwd.ExecTask(context.Background(), &proto.TaskRequest{
		Target:     "*",
		TargetMode: proto.TargetMode_GLOB,
		Task:       "cmd.run",
		Timeout:    5,
	})
	require.NoError(t, err)

	assert.Equal(t, proto.InternalError_OK, resp.GetResponses()["node1"].GetInternalError())
	assert.Equal(t, proto.InternalError_OK, resp.GetResponses()["node2"].GetInternalError())

	stream1.cancel()
	stream2.cancel()
	<-srvErrCh1
	<-srvErrCh2
}

// TestE2E_MultinodeMixedResults verifies that when targeting multiple nodes, each node's
// result is independent: one can succeed while another is disconnected.
func TestE2E_MultinodeMixedResults(t *testing.T) {
	h := newHarness(t)
	stream1, srvErrCh1 := h.connectNode(t, "node1")
	// node2 is deliberately not connected.

	go func() {
		req, err := stream1.nodeRecv(2 * time.Second)
		if err != nil {
			return
		}
		stream1.nodeReply(req, []byte(`"ok"`))
	}()

	resp, err := h.fwd.ExecTask(context.Background(), &proto.TaskRequest{
		Target:     "node1,node2",
		TargetMode: proto.TargetMode_LIST,
		Task:       "cmd.run",
		Timeout:    5,
	})
	require.NoError(t, err)

	assert.Equal(t, proto.InternalError_OK, resp.GetResponses()["node1"].GetInternalError())
	assert.Equal(t, proto.InternalError_DISCONNECTED, resp.GetResponses()["node2"].GetInternalError())

	stream1.cancel()
	<-srvErrCh1
}

// TestE2E_ConcurrentRequests verifies that two simultaneous callers targeting the same node
// each receive their own response, with no cross-contamination between in-flight tasks.
func TestE2E_ConcurrentRequests(t *testing.T) {
	h := newHarness(t)
	stream, srvErrCh := h.connectNode(t, "node1")

	// Node echoes the task name back as output so each caller can verify they got the right response.
	go func() {
		for {
			req, err := stream.nodeRecv(5 * time.Second)
			if err != nil {
				return
			}
			stream.nodeReply(req, []byte(`"`+req.GetTask()+`"`))
		}
	}()

	type result struct {
		resp *proto.FwdResponse
		task string
	}
	ch1 := make(chan result, 1)
	ch2 := make(chan result, 1)

	go func() {
		resp, _ := h.execTask(context.Background(), "node1", "plugin.taskA", 5)
		ch1 <- result{resp, "plugin.taskA"}
	}()
	go func() {
		resp, _ := h.execTask(context.Background(), "node1", "plugin.taskB", 5)
		ch2 <- result{resp, "plugin.taskB"}
	}()

	r1 := <-ch1
	r2 := <-ch2

	assert.Equal(t, proto.InternalError_OK, r1.resp.GetResponses()["node1"].GetInternalError())
	assert.Equal(t, proto.InternalError_OK, r2.resp.GetResponses()["node1"].GetInternalError())
	assert.Equal(t, []byte(`"`+r1.task+`"`), r1.resp.GetResponses()["node1"].GetOutput())
	assert.Equal(t, []byte(`"`+r2.task+`"`), r2.resp.GetResponses()["node1"].GetOutput())

	stream.cancel()
	<-srvErrCh
}

// TestE2E_NodeReconnects verifies that after a node disconnects and reconnects, tasks
// are routed correctly again through the new stream.
func TestE2E_NodeReconnects(t *testing.T) {
	h := newHarness(t)

	// First connection — disconnect immediately without sending any tasks.
	stream1, srvErrCh1 := h.connectNode(t, "node1")
	stream1.nodeDisconnect()
	stream1.cancel()
	<-srvErrCh1

	// Wait for the dispatcher to finish unregistering.
	require.Eventually(t, func() bool {
		nodes, _ := h.dispatcher.TargetedNodes("node1", proto.TargetMode_EXACT)
		return !nodes["node1"]
	}, 2*time.Second, 10*time.Millisecond, "node1 never unregistered after disconnect")

	// Second connection — node is already in the inventory (registered), just reconnects.
	stream2, srvErrCh2 := h.reconnectNode(t, "node1")

	go func() {
		req, err := stream2.nodeRecv(2 * time.Second)
		if err != nil {
			return
		}
		stream2.nodeReply(req, []byte(`"reconnected"`))
	}()

	resp, err := h.execTask(context.Background(), "node1", "cmd.run", 5)
	require.NoError(t, err)

	nodeResp := resp.GetResponses()["node1"]
	require.NotNil(t, nodeResp)
	assert.Equal(t, proto.InternalError_OK, nodeResp.GetInternalError())
	assert.Equal(t, []byte(`"reconnected"`), nodeResp.GetOutput())

	stream2.cancel()
	<-srvErrCh2
}

// TestE2E_TaskErrorResponse verifies that a module-level error returned by the node
// (non-zero retcode, error message) is faithfully propagated to the caller.
func TestE2E_TaskErrorResponse(t *testing.T) {
	h := newHarness(t)
	stream, srvErrCh := h.connectNode(t, "node1")

	go func() {
		req, err := stream.nodeRecv(2 * time.Second)
		if err != nil {
			return
		}
		stream.fromNode <- &proto.TaskResponse{
			Id:            req.GetId(),
			GroupID:       req.GroupID,
			InternalError: proto.InternalError_OK,
			ModuleError:   "permission denied",
			Retcode:       1,
		}
	}()

	resp, err := h.execTask(context.Background(), "node1", "cmd.run", 5)
	require.NoError(t, err)

	nodeResp := resp.GetResponses()["node1"]
	require.NotNil(t, nodeResp)
	assert.Equal(t, "permission denied", nodeResp.GetModuleError())
	assert.Equal(t, int32(1), nodeResp.GetRetcode())

	stream.cancel()
	<-srvErrCh
}

// TestE2E_CallerCancelledAfterSend verifies what happens when the caller cancels its context
// after the request has already been forwarded to the node.
//
// The forwarder currently does not propagate context cancellation: it continues to wait
// for the task response until the task timeout fires. This test documents that behaviour.
func TestE2E_CallerCancelledAfterSend(t *testing.T) {
	h := newHarness(t)
	stream, srvErrCh := h.connectNode(t, "node1")

	callerCtx, callerCancel := context.WithCancel(context.Background())
	resultCh := make(chan *proto.FwdResponse, 1)
	go func() {
		resp, _ := h.execTask(callerCtx, "node1", "cmd.run", 2) // 2s timeout
		resultCh <- resp
	}()

	// Wait for the task to arrive at the node, then cancel the caller.
	req, err := stream.nodeRecv(2 * time.Second)
	require.NoError(t, err, "task never reached the node")
	callerCancel()

	// The node responds after the caller cancelled — the forwarder should still
	// deliver the response (because context cancellation is not propagated).
	time.Sleep(100 * time.Millisecond)
	stream.nodeReply(req, []byte(`"late reply"`))

	select {
	case resp := <-resultCh:
		nodeResp := resp.GetResponses()["node1"]
		require.NotNil(t, nodeResp)
		// Response is returned even though the caller already cancelled.
		assert.Equal(t, proto.InternalError_OK, nodeResp.GetInternalError())
	case <-time.After(5 * time.Second):
		t.Fatal("forwarder did not return")
	}

	stream.cancel()
	<-srvErrCh
}
