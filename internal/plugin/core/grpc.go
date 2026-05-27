package core

import (
	"context"

	"github.com/kpetremann/jackadi/internal/plugin/core/protoplugin"
	"github.com/kpetremann/jackadi/internal/proto"
	empty "google.golang.org/protobuf/types/known/emptypb"
)

type GRPCClient struct {
	client protoplugin.JackadiPluginClient
}

func (c *GRPCClient) Name() (string, error) {
	result, err := c.client.Name(context.Background(), nil)
	return result.GetName(), err
}

func (c *GRPCClient) Tasks() ([]string, error) {
	result, err := c.client.Tasks(context.Background(), nil)
	return result.GetNames(), err
}

func (c *GRPCClient) Help(task string) (map[string]string, error) {
	result, err := c.client.Help(context.Background(), &protoplugin.HelpRequest{Task: task})
	return result.GetOutput(), err
}

func (c *GRPCClient) Version() (Version, error) {
	result, err := c.client.Version(context.Background(), nil)
	if err != nil {
		return Version{}, err
	}
	return Version{
		PluginVersion: result.GetPluginVersion(),
		Commit:        result.GetCommit(),
		BuildTime:     result.GetBuildTime(),
		GoVersion:     result.GetGoVersion(),
	}, nil
}

func (c *GRPCClient) Do(ctx context.Context, task string, input *proto.Input) (Response, error) {
	result, err := c.client.Do(ctx, &protoplugin.DoRequest{
		Task:  task,
		Input: input,
	})

	if err != nil {
		return Response{}, err
	}

	return Response{
		Output:  result.Output,
		Error:   result.Error,
		Retcode: result.Retcode,
	}, nil
}

func (c *GRPCClient) CollectSpecs(ctx context.Context) ([]byte, error) {
	r, err := c.client.CollectSpecs(ctx, nil)
	return r.GetOutput(), err
}

func (c *GRPCClient) GetTaskLockMode(task string) (proto.LockMode, error) {
	result, err := c.client.GetTaskLockMode(context.Background(), &protoplugin.TaskLockModeRequest{Task: task})
	if err != nil {
		return proto.LockMode_NO_LOCK, err
	}
	return result.GetLockMode(), nil
}

type GRPCServer struct {
	Impl Plugin
}

func (s *GRPCServer) Do(ctx context.Context, req *protoplugin.DoRequest) (*protoplugin.DoResponse, error) {
	result, err := s.Impl.Do(ctx, req.GetTask(), req.GetInput())

	resp := protoplugin.DoResponse{
		Output:  result.Output,
		Error:   result.Error,
		Retcode: result.Retcode,
	}

	return &resp, err
}

func (s *GRPCServer) CollectSpecs(ctx context.Context, req *empty.Empty) (*protoplugin.CollectSpecsResponse, error) {
	result, err := s.Impl.CollectSpecs(ctx)
	return &protoplugin.CollectSpecsResponse{Output: result}, err
}

func (s *GRPCServer) Name(ctx context.Context, req *empty.Empty) (*protoplugin.NameResponse, error) {
	name, err := s.Impl.Name()
	return &protoplugin.NameResponse{
		Name: name,
	}, err
}

func (s *GRPCServer) Tasks(ctx context.Context, req *empty.Empty) (*protoplugin.TasksResponse, error) {
	names, err := s.Impl.Tasks()
	return &protoplugin.TasksResponse{
		Names: names,
	}, err
}

func (s *GRPCServer) Help(ctx context.Context, req *protoplugin.HelpRequest) (*protoplugin.HelpResponse, error) {
	out, err := s.Impl.Help(req.GetTask())
	return &protoplugin.HelpResponse{
		Output: out,
	}, err
}

func (s *GRPCServer) Version(ctx context.Context, req *empty.Empty) (*protoplugin.VersionResponse, error) {
	out, err := s.Impl.Version()
	return &protoplugin.VersionResponse{
		PluginVersion: out.PluginVersion,
		Commit:        out.PluginVersion,
		BuildTime:     out.BuildTime,
		GoVersion:     out.GoVersion,
	}, err
}

func (s *GRPCServer) GetTaskLockMode(ctx context.Context, req *protoplugin.TaskLockModeRequest) (*protoplugin.TaskLockModeResponse, error) {
	lockMode, err := s.Impl.GetTaskLockMode(req.GetTask())
	if err != nil {
		return &protoplugin.TaskLockModeResponse{LockMode: proto.LockMode_NO_LOCK}, err
	}
	return &protoplugin.TaskLockModeResponse{LockMode: lockMode}, nil
}
