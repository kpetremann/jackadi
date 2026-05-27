package builtin

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strings"

	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/plugin/core"
	"github.com/kpetremann/jackadi/internal/plugin/inventory"
	"github.com/kpetremann/jackadi/internal/plugin/types"
	"github.com/kpetremann/jackadi/sdk"
)

func parseNames(name string) (string, string, error) {
	splitName := strings.Split(name, config.PluginSeparator)
	if len(splitName) == 0 {
		return "", "", errors.New("missing plugin or plugin.task")
	}
	pluginName := splitName[0]

	if pluginName == "" {
		return "", "", errors.New("missing plugin or plugin.task")
	}

	taskName := ""
	if len(splitName) > 1 {
		taskName = splitName[1]
	}

	return pluginName, taskName, nil
}

type pluginMgmt struct {
	req  chan struct{}
	resp chan types.PluginUpdateResponse
}

func (s pluginMgmt) help(name string) (map[string]string, error) {
	pluginName, taskName, err := parseNames(name)
	if err != nil {
		return nil, err
	}

	c, err := inventory.Registry.Get(pluginName)
	if err != nil {
		return nil, fmt.Errorf("unknown plugin: %s", pluginName)
	}

	return c.Help(taskName)
}

func (s pluginMgmt) version(pluginName string) (*core.Version, error) {
	c, err := inventory.Registry.Get(pluginName)
	if err != nil {
		return nil, fmt.Errorf("unknown plugin: %w", err)
	}

	info, err := c.Version()
	if err != nil {
		return nil, fmt.Errorf("failed to get version: %w", err)
	}

	out := core.Version{
		PluginVersion: info.PluginVersion,
		Commit:        info.Commit,
		BuildTime:     info.BuildTime,
		GoVersion:     info.GoVersion,
	}

	return &out, nil
}

func (s pluginMgmt) list() ([]string, error) {
	return inventory.Registry.Names(), nil
}

type pluginInfo struct {
	Name string
	File *string `jackadi:"File,omitempty"`
}

type diff struct {
	Added     []pluginInfo `jackadi:"Added,omitempty"`
	Unchanged []pluginInfo `jackadi:"Unchanged,omitempty"`
	Deleted   []pluginInfo `jackadi:"Deleted,omitempty"`
	Updated   []pluginInfo `jackadi:"Updated,omitempty"`
}

func (s pluginMgmt) sync() (*diff, error) {
	s.req <- struct{}{} // request update to KeepPluginsUpToDate goroutine (internal/node/plugins.go)
	changes := <-s.resp

	out := diff{}
	for _, p := range changes.Changes {
		info := pluginInfo{Name: p.Name}
		if p.Name != p.FileName {
			info.File = &p.FileName
		}

		switch {
		case p.New:
			out.Added = append(out.Added, info)
		case p.Updated:
			out.Updated = append(out.Updated, info)
		case p.Deleted:
			out.Deleted = append(out.Deleted, info)
		default:
			out.Unchanged = append(out.Unchanged, info)
		}
	}

	return &out, nil
}

func MustLoadPluginMgmt(req chan struct{}) chan types.PluginUpdateResponse {
	resp := make(chan types.PluginUpdateResponse)
	plugingMgmt := pluginMgmt{req: req, resp: resp}

	c := sdk.New("plugins")
	c.MustRegisterTask("help", plugingMgmt.help).
		WithSummary("Provide help for the given plugin or plugin:task.").
		WithDescription("Help for 'plugin': gives the list of task with their summary.\nHelp for 'plugin:task': gives the full details of the task.").
		WithArg("name", "plugin[:task]", "cmd or cmd:run")
	c.MustRegisterTask("version", plugingMgmt.version).
		WithSummary("Provide version of the given plugin.").
		WithDescription("Info for 'plugin': gives version, commit id, build time, go version.").
		WithArg("name", "plugin", "cmd")
	c.MustRegisterTask("list", plugingMgmt.list).
		WithSummary("List of task in the given plugin.").
		WithArg("name", "plugin", "cmd")
	c.MustRegisterTask("sync", plugingMgmt.sync).
		WithSummary("Sync plugin with the manager.").
		WithDescription("The node sync its plugins with the manager.\nIt adds, updates and removes the plugins following the manager configuration.").
		WithLockMode(sdk.ExclusiveLock)

	if err := inventory.Registry.Register(c); err != nil {
		name, _ := c.Name()
		slog.Error("could not load builtin task", "error", err, "task", name)
		log.Fatal(err)
	}

	return resp
}
