package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/helper"
	"github.com/kpetremann/jackadi/internal/manager/database"
	"github.com/kpetremann/jackadi/internal/manager/forwarder"
	"github.com/kpetremann/jackadi/internal/manager/inventory"
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/proto"
	"github.com/kpetremann/jackadi/internal/serializer"
	"google.golang.org/protobuf/types/known/structpb"
)

type ServerConfig struct {
	AutoAccept  bool
	MTLSEnabled bool
	ConfigDir   string
	PluginDir   string
}

type Server struct {
	proto.UnimplementedClusterServer
	config          ServerConfig
	Inventory       *inventory.Nodes
	taskDispatcher  forwarder.Dispatcher[*proto.TaskRequest, *proto.TaskResponse]
	db              *badger.DB
	dbMutex         *sync.Mutex
	shutdownRequest map[node.ID]chan struct{}
	shutdownMu      sync.RWMutex
	pluginPolicies  pluginPolicies
}

type pluginPolicies struct {
	cache      map[string][]pluginInfo // key: pattern
	lastUpdate time.Time
	lock       *sync.Mutex
}

func New(config ServerConfig, nodesInventory *inventory.Nodes, taskDispatcher forwarder.Dispatcher[*proto.TaskRequest, *proto.TaskResponse], jobDatabase *badger.DB) Server {
	return Server{
		config:          config,
		Inventory:       nodesInventory,
		taskDispatcher:  taskDispatcher,
		db:              jobDatabase,
		dbMutex:         &sync.Mutex{},
		shutdownRequest: make(map[node.ID]chan struct{}),
		pluginPolicies:  pluginPolicies{lock: &sync.Mutex{}},
	}
}

func (s *Server) RequestShutdown(nodeID node.ID) error {
	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()

	ch, ok := s.shutdownRequest[nodeID]
	if !ok {
		return errors.New("shutdownRequest channel not found")
	}
	if ch == nil {
		return errors.New("shutdownRequest channel not initialized")
	}

	close(ch)
	s.shutdownRequest[nodeID] = nil
	if err := s.taskDispatcher.Forget(nodeID); err != nil {
		slog.Error("fail to forget a node", "error", err)
	}
	return nil
}

// GetInventory returns the server's inventory.
func (s *Server) GetInventory() *inventory.Nodes {
	return s.Inventory
}

// storeResult records task responses in a local KV store. The KV store is an embedded Badger instance.
//
// It stores the result itself by task ID. It also stores a mapping between a group ID and task IDs.
// Group ID are grouping tasks response from a same request, i.e. when the request was targeting multiple nodes.
func (s *Server) storeResult(nodeID node.ID, msg *proto.TaskResponse) {
	s.dbMutex.Lock()
	defer s.dbMutex.Unlock()

	dbDerr := s.db.Update(func(txn *badger.Txn) error {
		data, err := database.MarshalTask(nodeID, msg)
		if err != nil {
			slog.Error("unable to record result", "error", "marshal error")
			return err
		}

		id := strconv.FormatInt(msg.GetId(), 10)
		if msg.GetId() == 0 {
			return nil
		}

		key := database.GenerateResultKey(id)
		singleEntry := badger.NewEntry(key, data).WithTTL(config.DBTaskResultTTL)
		if err := txn.SetEntry(singleEntry); err != nil {
			slog.Error("unable to record result", "error", err)
			return err
		}

		// map the result to the matching groupID
		if msg.GetGroupID() == 0 {
			slog.Debug("no need to group the result", "msg", "no group ID defined")
			return nil
		}
		groupID := strconv.FormatInt(msg.GetGroupID(), 10)
		groupKey := database.GenerateResultKey(groupID)
		item, err := txn.Get(groupKey)
		if err != nil {
			if !errors.Is(err, badger.ErrKeyNotFound) {
				slog.Error("unable to group the result", "error", "marshal error")
				return err
			}
			groupEntry := badger.NewEntry(groupKey, []byte("grouped:"+id)).WithTTL(config.DBTaskResultTTL)
			if err := txn.SetEntry(groupEntry); err != nil {
				slog.Error("unable to record the new group", "error", err)
				return err
			}
			return nil
		}
		groupEntry, err := item.ValueCopy(nil)
		if err != nil {
			slog.Error("unable to get value of existing group", "error", err)
			return err
		}

		groupEntry = append(groupEntry, []byte(","+id)...)
		if err := txn.Set(groupKey, groupEntry); err != nil {
			slog.Error("unable to get update the existing group", "error", err)
			return err
		}

		slog.Debug("updating group", "value", string(groupEntry))
		return nil
	})
	if dbDerr != nil {
		slog.Warn("failed to store task result", "error", dbDerr)
	}
}

// dispatchRequestsToNode waits for requests and sends them to the linked node.
func (s *Server) dispatchRequestsToNode(nodeID node.ID, stream proto.Cluster_ExecTaskServer, responsesCh map[int64]chan *proto.TaskResponse, responsesChLock *sync.Mutex) error {
	tasksCh, err := s.taskDispatcher.GetTasksChannel(nodeID)
	if err != nil {
		return err
	}

	for d := range tasksCh {
		ID := time.Now().UnixNano()
		err := stream.Send(
			&proto.TaskRequest{
				Id:      ID,
				GroupID: d.Request.GroupID,
				Task:    d.Request.Task,
				Input:   d.Request.GetInput(),
				Timeout: d.Request.Timeout,
			},
		)
		if err != nil {
			slog.Error("failed to send task", "err", err, "node", nodeID)
			return err
		}
		responsesChLock.Lock()
		responsesCh[ID] = d.ResponseCh
		responsesChLock.Unlock()
		d := d
		go func() {
			// ensure cleaning to avoid memory leak when responses never received
			time.Sleep(time.Duration(d.Request.Timeout) * time.Second)
			responsesChLock.Lock()
			delete(responsesCh, ID)
			responsesChLock.Unlock()
		}()
	}
	return nil
}

// dispatchNodeResponse waits for a node's response and sends it back to the requester.
//
// It stores all received responses to the job database.
func (s *Server) dispatchNodeResponse(stream proto.Cluster_ExecTaskServer, nodeID node.ID, responsesCh map[int64]chan *proto.TaskResponse, responsesChLock *sync.Mutex) error {
	ctx := stream.Context()

	s.shutdownMu.RLock()
	shutdownCh := s.shutdownRequest[nodeID]
	s.shutdownMu.RUnlock()

	for {
		msg, err := stream.Recv()
		select {
		case <-ctx.Done():
			slog.Debug("stream context cancelled", "node", nodeID, "error", ctx.Err())
			return ctx.Err()
		case <-shutdownCh:
			slog.Warn("shutdown request received", "node", nodeID)
			slog.Debug("ignored response because received after shutdown request", "node", nodeID)
			return errors.New("shutdown requested")
		default:
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			slog.Error("connection with node stopped", "node", nodeID, "error", err)
			return err
		}

		slog.Debug("received task response", "id", msg.GetId(), "node", nodeID, "group", msg.GetGroupID())
		if msg.GetInternalError() != proto.InternalError_STARTED_TIMEOUT {
			// we don't store the message if the task has started to avoid duplicate entries if the task finishes after the timeout
			s.storeResult(nodeID, msg)
		}

		if msg.GetInternalError() == proto.InternalError_OK {
			s.Inventory.MarkNodeActive(nodeID)
		}

		responsesChLock.Lock()
		ch, ok := responsesCh[msg.GetId()]
		responsesChLock.Unlock()
		if ok {
			select {
			case ch <- msg:
			case <-time.After(config.ResponseChannelTimeout):
			}
			responsesChLock.Lock()
			delete(responsesCh, msg.GetId())
			responsesChLock.Unlock()
		} else {
			slog.Info("response not sent to the caller", "err", "response channel not found", "node", nodeID, "id", msg.GetId())
		}

		if msg.GetInternalError() > 0 {
			slog.Error("the node rejected the request", "error", msg.GetInternalError(), "message", msg.GetModuleError(), "request_id", msg.GetId(), "node", nodeID)
		}
	}
}

// ExecTask spawns a stream with each node.
//
// It handles the sending of requests and the routing of the response.
// The responses are routed to the dispatcher (usually the forwarder).
func (s *Server) ExecTask(stream proto.Cluster_ExecTaskServer) error {
	nd, err := signatureFromContext(stream.Context(), s.config.MTLSEnabled)
	if err != nil {
		return err
	}
	slog.Debug("new 'task' stream with node", "node", nd.ID)
	s.shutdownMu.Lock()
	s.shutdownRequest[nd.ID] = make(chan struct{})
	s.shutdownMu.Unlock()
	defer func() {
		s.shutdownMu.Lock()
		delete(s.shutdownRequest, nd.ID)
		s.shutdownMu.Unlock()
	}()

	for !s.Inventory.IsRegistered(nd) {
		slog.Debug("cannot start 'Task' stream", "error", "node not registered", "retry_in", fmt.Sprintf("%d", int(config.NodeRetryDelay.Seconds())))
		time.Sleep(config.NodeRetryDelay)
	}

	if err := s.taskDispatcher.RegisterNode(nd.ID); err != nil {
		slog.Warn("closing stream", "msg", err, "node", nd.ID, "peer", nd.Address)
		return err
	}
	s.Inventory.MarkNodeStateChange(nd.ID, true)

	defer func() {
		slog.Debug("deleting node dispatcher", "node", nd.ID)
		s.taskDispatcher.UnregisterNode(nd.ID)
		s.Inventory.MarkNodeStateChange(nd.ID, false)
	}()

	responsesCh := make(map[int64]chan *proto.TaskResponse)
	lock := &sync.Mutex{} // TODO: replace map[]chan by a single thread channel manager
	errCh := make(chan error)

	go func() {
		err := s.dispatchNodeResponse(stream, nd.ID, responsesCh, lock)
		slog.Debug("closing node dispatcher", "node", nd.ID)
		s.taskDispatcher.Close(nd.ID)
		slog.Debug("node dispatcher closed", "node", nd.ID)
		errCh <- err
	}()

	err = s.dispatchRequestsToNode(nd.ID, stream, responsesCh, lock)

	return errors.Join(err, <-errCh)
}

func (s *Server) CollectNodesSpecs(ctx context.Context) {
	timeout := config.TaskTimeout
	tick := time.NewTicker(config.SpecCollectionInterval)
	for {
		nodes, _ := s.taskDispatcher.TargetedNodes("*", proto.TargetMode_GLOB)

		wg := sync.WaitGroup{}
		slog.Debug("starting nodes specs collector")
		for nd, connected := range nodes {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if !connected {
				slog.Debug("cannot get spec of node", "node", nd, "error", "disconnected")
				continue
			}

			wg.Add(1)
			go func() {
				slog.Debug("collecting specs", "node", nd)
				defer wg.Done()

				req := &proto.TaskRequest{
					Task:    config.SpecManagerPrefix + config.PluginSeparator + "all",
					Input:   &proto.Input{Args: &structpb.ListValue{}},
					Timeout: helper.DurationToUint32(timeout),
				}

				resp := make(chan *proto.TaskResponse, 1)
				task := forwarder.Task[*proto.TaskRequest, *proto.TaskResponse]{
					Request:    req,
					ResponseCh: resp,
				}
				// timeout+time.Second to get last moment response
				if err := s.taskDispatcher.Send(node.ID(nd), task, timeout+time.Second); err != nil {
					slog.Warn("failed to send collect specs request", "node", nd, "error", err)
					close(resp)
					return
				}

				var res *proto.TaskResponse
				select {
				case res = <-resp:
				case <-time.After(timeout + 30*time.Second):
					slog.Warn("timeout waiting for specs", "node", nd)
					return
				}
				if res.GetError() != "" {
					slog.Warn("failed to collect specs", "node", nd, "error", res.GetError())
					return
				}

				specs := make(map[string]any)
				if err := serializer.JSON.Unmarshal(res.GetOutput(), &specs); err != nil {
					slog.Warn("failed to parse specs", "node", nd, "error", err)
					return
				}

				if err := s.Inventory.SetSpec(node.ID(nd), specs); err != nil {
					slog.Warn("failed to set specs", "node", nd, "error", err)
					return
				}

				slog.Debug("specs collected", "node", nd)
			}()
		}
		wg.Wait()

		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
	}
}
