package core

import (
	"context"
	"fmt"

	"github.com/kpetremann/jackadi/internal/proto"
)

type Plugin interface {
	Name() (string, error)
	Tasks() ([]string, error)
	Help(task string) (map[string]string, error)
	Version() (Version, error)
	Do(ctx context.Context, task string, input *proto.Input) (Response, error)
	CollectSpecs(ctx context.Context) ([]byte, error)
	GetTaskLockMode(task string) (proto.LockMode, error)
}

type Response struct {
	Output  []byte
	Error   string
	Retcode int32
}

type Version struct {
	PluginVersion string
	Commit        string
	BuildTime     string
	GoVersion     string
}

func (v Version) String() string {
	return fmt.Sprintf("Version: %s\nCommit: %s\nBuild time: %s\nGo version: %s",
		v.PluginVersion, v.Commit, v.BuildTime, v.GoVersion)
}
