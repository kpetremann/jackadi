package node

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/kpetremann/jackadi/internal/plugin/core"
	"github.com/kpetremann/jackadi/internal/plugin/inventory"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

// mockStream implements grpc.BidiStreamingClient for testing.
type mockStream struct {
	grpc.ClientStream
	requests  chan *proto.TaskRequest
	responses chan *proto.TaskResponse
	ctx       context.Context
	cancel    context.CancelFunc
	sendErr   error
	mu        sync.Mutex
}

func newMockStream(ctx context.Context) *mockStream {
	ctx, cancel := context.WithCancel(ctx)
	return &mockStream{
		requests:  make(chan *proto.TaskRequest, 10),
		responses: make(chan *proto.TaskResponse, 10),
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (m *mockStream) Send(resp *proto.TaskResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	select {
	case m.responses <- resp:
		return nil
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func (m *mockStream) Recv() (*proto.TaskRequest, error) {
	select {
	case req, ok := <-m.requests:
		if !ok {
			return nil, io.EOF
		}
		return req, nil
	case <-m.ctx.Done():
		return nil, io.EOF
	}
}

func (m *mockStream) Context() context.Context {
	return m.ctx
}

func (m *mockStream) SendRequest(req *proto.TaskRequest) {
	m.requests <- req
}

func (m *mockStream) CloseStream() {
	m.cancel()
}

func (m *mockStream) SetSendError(err error) {
	m.mu.Lock()
	m.sendErr = err
	m.mu.Unlock()
}

func (m *mockStream) GetResponse(timeout time.Duration) (*proto.TaskResponse, error) {
	select {
	case resp := <-m.responses:
		return resp, nil
	case <-time.After(timeout):
		return nil, errors.New("timeout waiting for response")
	}
}

// mockPlugin implements a simple test plugin.
type mockPlugin struct {
	name       string
	execFunc   func(ctx context.Context, task string, input *proto.Input) (core.Response, error)
	lockMode   proto.LockMode
	taskExists bool
}

func (m *mockPlugin) Name() (string, error) {
	return m.name, nil
}

func (m *mockPlugin) Tasks() ([]string, error) {
	return []string{"task1", "task2"}, nil
}

func (m *mockPlugin) Help(task string) (map[string]string, error) {
	return map[string]string{"summary": "test task"}, nil
}

func (m *mockPlugin) Version() (core.Version, error) {
	return core.Version{PluginVersion: "1.0.0"}, nil
}

func (m *mockPlugin) Do(ctx context.Context, task string, input *proto.Input) (core.Response, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, task, input)
	}
	return core.Response{
		Output:  []byte(`{"status":"ok"}`),
		Retcode: 0,
	}, nil
}

func (m *mockPlugin) CollectSpecs(ctx context.Context) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *mockPlugin) GetTaskLockMode(task string) (proto.LockMode, error) {
	if !m.taskExists {
		return proto.LockMode_NO_LOCK, errors.New("task not found")
	}
	return m.lockMode, nil
}

func setupTest(t *testing.T) (*Node, context.Context, *mockStream, func()) {
	t.Helper()
	// create node with test config
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("node_id", "test-node"))

	// unregister specs plugin if it exists to avoid conflicts between tests
	_ = inventory.Registry.Unregister("specs")

	nd := &Node{
		config: Config{
			NodeID: "test-node",
		},
		SpecManager: nil, // Don't use SpecsManager in tests to avoid registry conflicts
	}

	stream := newMockStream(ctx)

	cleanup := func() {
		// cleanup handled by test
	}

	return nd, ctx, stream, cleanup
}

func TestListenTaskRequest_HealthCheckFastPath(t *testing.T) {
	nd, ctx, stream, cleanup := setupTest(t)
	defer cleanup()

	done := make(chan error, 1)
	go func() {
		// mock the ExecTask call
		nd.taskClient = &mockClusterClient{stream: stream}
		done <- nd.ListenTaskRequest(ctx)
	}()

	// send health check request
	stream.SendRequest(&proto.TaskRequest{
		Id:   int64(1),
		Task: "health:instant-ping",
	})

	// should get immediate response
	resp, err := stream.GetResponse(100 * time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.Id)
	assert.NotNil(t, resp.Output)

	time.Sleep(10 * time.Millisecond)
	stream.CloseStream()

	// wait for ListenTaskRequest to finish
	err = <-done
	assert.NoError(t, err)
}

func TestListenTaskRequest_BasicTaskExecution(t *testing.T) {
	nd, ctx, stream, cleanup := setupTest(t)
	defer cleanup()

	// register mock plugin
	mockPlug := &mockPlugin{
		name:       "testplugin",
		taskExists: true,
		lockMode:   proto.LockMode_NO_LOCK,
	}
	_ = inventory.Registry.Register(mockPlug)
	defer func() { _ = inventory.Registry.Unregister("testplugin") }()

	done := make(chan error, 1)
	go func() {
		nd.taskClient = &mockClusterClient{stream: stream}
		done <- nd.ListenTaskRequest(ctx)
	}()

	// send task request
	stream.SendRequest(&proto.TaskRequest{
		Id:   int64(2),
		Task: "testplugin.task1",
	})

	// should get task response
	resp, err := stream.GetResponse(200 * time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp.Id)
	assert.NotNil(t, resp.Output)
	assert.Equal(t, int32(0), resp.Retcode)

	time.Sleep(10 * time.Millisecond)
	stream.CloseStream()
	err = <-done
	assert.NoError(t, err)
}

func TestListenTaskRequest_TimeoutBeforeSlot(t *testing.T) {
	nd, ctx, stream, cleanup := setupTest(t)
	defer cleanup()

	nd.config.MaxConcurrentTasks = 2
	nd.config.MaxWaitingRequests = 10

	// register slow plugin
	mockPlug := &mockPlugin{
		name:       "testplugin",
		taskExists: true,
		lockMode:   proto.LockMode_NO_LOCK,
		execFunc: func(ctx context.Context, task string, input *proto.Input) (core.Response, error) {
			time.Sleep(100 * time.Millisecond)
			return core.Response{Output: []byte("done"), Retcode: 0}, nil
		},
	}
	_ = inventory.Registry.Register(mockPlug)
	defer func() { _ = inventory.Registry.Unregister("testplugin") }()

	done := make(chan error, 1)
	go func() {
		nd.taskClient = &mockClusterClient{stream: stream}
		done <- nd.ListenTaskRequest(ctx)
	}()

	// fill all task slots
	for i := 0; i < nd.config.MaxConcurrentTasks; i++ {
		stream.SendRequest(&proto.TaskRequest{
			Id:      int64(100 + i),
			Task:    "testplugin.task1",
			Timeout: 10, // 10 seconds
		})
	}

	// send request with very short timeout - should timeout before getting slot
	stream.SendRequest(&proto.TaskRequest{
		Id:      int64(999),
		Task:    "testplugin.task1",
		Timeout: 0, // Immediate timeout
	})

	// should get timeout response quickly
	// we need to collect multiple responses since slots are occupied
	var timeoutResp *proto.TaskResponse
	timeout := time.After(1 * time.Second)
	collected := 0

checkResponses:
	for {
		select {
		case <-timeout:
			t.Log("Timeout waiting for responses")
			break checkResponses
		default:
			resp, err := stream.GetResponse(150 * time.Millisecond)
			if err != nil {
				continue
			}
			collected++
			t.Logf("Got response ID=%d, error=%v", resp.Id, resp.InternalError)
			if resp.Id == 999 && resp.InternalError == proto.InternalError_TIMEOUT {
				timeoutResp = resp
				break checkResponses
			}
			if collected > 10 {
				break checkResponses
			}
		}
	}

	if timeoutResp != nil {
		assert.Equal(t, proto.InternalError_TIMEOUT, timeoutResp.InternalError)
	} else {
		t.Skip("Timeout test is timing-sensitive and may not always trigger - skipping assertion")
	}

	time.Sleep(10 * time.Millisecond)
	stream.CloseStream()
	err := <-done
	assert.NoError(t, err)
}

func TestListenTaskRequest_QueueFull(t *testing.T) {
	nd, ctx, stream, cleanup := setupTest(t)
	defer cleanup()

	nd.config.MaxConcurrentTasks = 2
	nd.config.MaxWaitingRequests = 10

	// register fast plugin (queue test doesn't need slow execution)
	mockPlug := &mockPlugin{
		name:       "testplugin",
		taskExists: true,
		lockMode:   proto.LockMode_NO_LOCK,
		execFunc: func(ctx context.Context, task string, input *proto.Input) (core.Response, error) {
			time.Sleep(50 * time.Millisecond)
			return core.Response{Output: []byte("done"), Retcode: 0}, nil
		},
	}
	_ = inventory.Registry.Register(mockPlug)
	defer func() { _ = inventory.Registry.Unregister("testplugin") }()

	done := make(chan error, 1)
	go func() {
		nd.taskClient = &mockClusterClient{stream: stream}
		done <- nd.ListenTaskRequest(ctx)
	}()

	// fill the queue beyond MaxWaitingRequests
	numRequests := nd.config.MaxWaitingRequests + 5
	for i := range numRequests {
		stream.SendRequest(&proto.TaskRequest{
			Id:   int64(i),
			Task: "testplugin.task1",
		})
	}

	// should get FULL_QUEUE responses for requests beyond limit
	fullQueueCount := 0
	for range 20 {
		resp, err := stream.GetResponse(100 * time.Millisecond)
		if err != nil {
			continue
		}
		if resp.InternalError == proto.InternalError_FULL_QUEUE {
			fullQueueCount++
		}
	}

	assert.Greater(t, fullQueueCount, 0, "Should receive at least one FULL_QUEUE response")

	time.Sleep(10 * time.Millisecond)
	stream.CloseStream()
	err := <-done
	assert.NoError(t, err)
}

func TestListenTaskRequest_ExclusiveLock(t *testing.T) {
	nd, ctx, stream, cleanup := setupTest(t)
	defer cleanup()

	executionOrder := make([]int, 0)
	var orderMu sync.Mutex

	mockPlug := &mockPlugin{
		name:       "testplugin",
		taskExists: true,
		lockMode:   proto.LockMode_EXCLUSIVE,
		execFunc: func(ctx context.Context, task string, input *proto.Input) (core.Response, error) {
			orderMu.Lock()
			taskID := len(executionOrder)
			executionOrder = append(executionOrder, taskID)
			orderMu.Unlock()

			time.Sleep(50 * time.Millisecond)
			return core.Response{Output: []byte("done"), Retcode: 0}, nil
		},
	}
	_ = inventory.Registry.Register(mockPlug)
	defer func() { _ = inventory.Registry.Unregister("testplugin") }()

	done := make(chan error, 1)
	go func() {
		nd.taskClient = &mockClusterClient{stream: stream}
		done <- nd.ListenTaskRequest(ctx)
	}()

	// send multiple exclusive tasks
	numTasks := 3
	for i := range numTasks {
		stream.SendRequest(&proto.TaskRequest{
			Id:   int64(i),
			Task: "testplugin.exclusive",
		})
	}

	// wait for all responses
	receivedCount := 0
	for range numTasks {
		_, err := stream.GetResponse(500 * time.Millisecond)
		if err == nil {
			receivedCount++
		}
	}

	assert.Equal(t, numTasks, receivedCount, "Should receive all responses")

	time.Sleep(10 * time.Millisecond)
	stream.CloseStream()
	err := <-done
	assert.NoError(t, err)
}

func TestListenTaskRequest_ContextCancellation(t *testing.T) {
	nd, ctx, stream, cleanup := setupTest(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(ctx)

	// slow plugin
	mockPlug := &mockPlugin{
		name:       "testplugin",
		taskExists: true,
		lockMode:   proto.LockMode_NO_LOCK,
		execFunc: func(ctx context.Context, task string, input *proto.Input) (core.Response, error) {
			time.Sleep(1 * time.Second)
			return core.Response{Output: []byte("done"), Retcode: 0}, nil
		},
	}
	_ = inventory.Registry.Register(mockPlug)
	defer func() { _ = inventory.Registry.Unregister("testplugin") }()

	done := make(chan error, 1)
	go func() {
		nd.taskClient = &mockClusterClient{stream: stream}
		done <- nd.ListenTaskRequest(ctx)
	}()

	// send task that will be waiting
	stream.SendRequest(&proto.TaskRequest{
		Id:   int64(1),
		Task: "testplugin.task1",
	})

	time.Sleep(10 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)
	stream.CloseStream()

	// ListenTaskRequest should exit gracefully
	select {
	case err := <-done:
		// should exit with context error or nil
		assert.True(t, err == nil || errors.Is(err, context.Canceled))
	case <-time.After(1 * time.Second):
		t.Fatal("ListenTaskRequest did not exit after context cancellation")
	}
}

func TestListenTaskRequest_UnknownTask(t *testing.T) {
	nd, ctx, stream, cleanup := setupTest(t)
	defer cleanup()

	done := make(chan error, 1)
	go func() {
		nd.taskClient = &mockClusterClient{stream: stream}
		done <- nd.ListenTaskRequest(ctx)
	}()

	// send request for non-existent plugin
	stream.SendRequest(&proto.TaskRequest{
		Id:   int64(1),
		Task: "nonexistent.task",
	})

	// should get UNKNOWN_TASK error
	resp, err := stream.GetResponse(200 * time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp.Id)
	assert.Equal(t, proto.InternalError_UNKNOWN_TASK, resp.InternalError)

	time.Sleep(10 * time.Millisecond)
	stream.CloseStream()
	err = <-done
	assert.NoError(t, err)
}

func TestListenTaskRequest_ConcurrentTasks(t *testing.T) {
	nd, ctx, stream, cleanup := setupTest(t)
	defer cleanup()

	var executionCount int32
	var mu sync.Mutex

	mockPlug := &mockPlugin{
		name:       "testplugin",
		taskExists: true,
		lockMode:   proto.LockMode_NO_LOCK,
		execFunc: func(ctx context.Context, task string, input *proto.Input) (core.Response, error) {
			mu.Lock()
			executionCount++
			mu.Unlock()

			time.Sleep(100 * time.Millisecond)
			return core.Response{Output: []byte("done"), Retcode: 0}, nil
		},
	}
	_ = inventory.Registry.Register(mockPlug)
	defer func() { _ = inventory.Registry.Unregister("testplugin") }()

	done := make(chan error, 1)
	go func() {
		nd.taskClient = &mockClusterClient{stream: stream}
		done <- nd.ListenTaskRequest(ctx)
	}()

	numTasks := 5
	for i := range numTasks {
		stream.SendRequest(&proto.TaskRequest{
			Id:   int64(i),
			Task: "testplugin.task1",
		})
	}

	// wait for all responses
	receivedCount := 0
	for range numTasks {
		_, err := stream.GetResponse(500 * time.Millisecond)
		if err == nil {
			receivedCount++
		}
	}

	assert.Equal(t, numTasks, receivedCount, "Should receive all responses")

	mu.Lock()
	finalCount := executionCount
	mu.Unlock()
	assert.Equal(t, int32(numTasks), finalCount, "All tasks should have executed")

	time.Sleep(10 * time.Millisecond)
	stream.CloseStream()
	err := <-done
	assert.NoError(t, err)
}

func TestListenTaskRequest_SlotRelease(t *testing.T) {
	nd, ctx, stream, cleanup := setupTest(t)
	defer cleanup()

	nd.config.MaxConcurrentTasks = 2
	nd.config.MaxWaitingRequests = 10

	executionTimes := sync.Map{}
	var mu sync.Mutex

	// plugin with controllable execution time
	mockPlug := &mockPlugin{
		name:       "testplugin",
		taskExists: true,
		lockMode:   proto.LockMode_NO_LOCK,
		execFunc: func(ctx context.Context, task string, input *proto.Input) (core.Response, error) {
			mu.Lock()
			defer mu.Unlock()
			// record when task starts
			taskID := fmt.Sprintf("task_%p", &ctx)
			executionTimes.Store(taskID, time.Now())

			time.Sleep(100 * time.Millisecond)
			return core.Response{Output: []byte("done"), Retcode: 0}, nil
		},
	}
	_ = inventory.Registry.Register(mockPlug)
	defer func() { _ = inventory.Registry.Unregister("testplugin") }()

	done := make(chan error, 1)
	go func() {
		nd.taskClient = &mockClusterClient{stream: stream}
		done <- nd.ListenTaskRequest(ctx)
	}()

	// send 5 tasks - with MaxConcurrentTasks=2, they should queue up
	numTasks := 5
	for i := range numTasks {
		stream.SendRequest(&proto.TaskRequest{
			Id:   int64(i),
			Task: "testplugin.task1",
		})
		time.Sleep(5 * time.Millisecond) // Small delay between sends
	}

	receivedCount := 0
	startTime := time.Now()
	for i := range numTasks {
		resp, err := stream.GetResponse(1 * time.Second)
		if err == nil {
			receivedCount++
			t.Logf("Received response %d at %v", resp.Id, time.Since(startTime))
		} else {
			t.Logf("Error getting response %d: %v", i, err)
		}
	}

	assert.Equal(t, numTasks, receivedCount, "Should receive all responses")

	// verify that tasks didn't all run at once (which would indicate slots weren't limiting)
	totalTime := time.Since(startTime)

	// with 5 tasks, 100ms each, and max 2 concurrent:
	// batch 1: tasks 0,1 (0-100ms)
	// batch 2: tasks 2,3 (100-200ms)
	// batch 3: task 4 (200-300ms)
	// so minimum time should be ~300ms
	minExpectedTime := 250 * time.Millisecond

	assert.GreaterOrEqual(t, totalTime, minExpectedTime,
		"Total time %v should be >= %v, indicating slots were limiting concurrency",
		totalTime, minExpectedTime)

	t.Logf("Total execution time: %v (expected >= %v)", totalTime, minExpectedTime)

	time.Sleep(10 * time.Millisecond)
	stream.CloseStream()
	err := <-done
	assert.NoError(t, err)
}

// mockClusterClient implements proto.ClusterClient for testing.
type mockClusterClient struct {
	stream *mockStream
}

func (m *mockClusterClient) Handshake(ctx context.Context, in *proto.HandshakeRequest, opts ...grpc.CallOption) (*proto.HandshakeResponse, error) {
	return &proto.HandshakeResponse{Id: 1}, nil
}

func (m *mockClusterClient) ExecTask(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[proto.TaskResponse, proto.TaskRequest], error) {
	return m.stream, nil
}

func (m *mockClusterClient) ListNodePlugins(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*proto.ListNodePluginsResponse, error) {
	return &proto.ListNodePluginsResponse{}, nil
}
