package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/plugin/builtin"
	"github.com/kpetremann/jackadi/internal/plugin/inventory"
	"github.com/kpetremann/jackadi/internal/plugin/loader/hcplugin"
	"github.com/kpetremann/jackadi/internal/plugin/loader/stdplugin"
	"github.com/kpetremann/jackadi/internal/plugin/types"
	"google.golang.org/protobuf/types/known/emptypb"
)

var mu sync.Mutex

func (n *Node) NodePlugins(ctx context.Context) (map[string]string, error) {
	res, err := n.taskClient.ListNodePlugins(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("server query failed: %w", err)
	}

	return res.GetPlugin(), nil
}

func LoadBuiltins(syncReq chan struct{}) chan types.PluginUpdateResponse {
	builtin.MustLoadCmd()
	builtin.MustLoadHealth()
	return builtin.MustLoadPluginMgmt(syncReq)
}

func (n *Node) KeepPluginsUpToDate(ctxMetadata context.Context, specsSync chan struct{}) {
	defer slog.Info("plugin manager closed")

	// Load Go standard plugin (not preferred)
	stdplugin.Load(n.config.PluginDir)

	// Load hashicorp type plugins
	hcplugins := hcplugin.New()
	hcplugins.Load(n.config.PluginDir)
	slog.Info("loaded plugins", "plugins", inventory.Registry.Names())

	mu.Lock()
	n.pluginLoader = hcplugins
	mu.Unlock()

	syncReq := make(chan struct{})
	resp := LoadBuiltins(syncReq)

	for {
		select {
		case <-syncReq:
			changes, changed, err := n.updatePlugins(ctxMetadata)
			ret := types.PluginUpdateResponse{Changes: changes, Error: err}
			resp <- ret
			if changed {
				specsSync <- struct{}{}
			}
		case <-ctxMetadata.Done():
			return
		}
	}
}

func (n *Node) updatePlugins(ctxMetadata context.Context) ([]types.PluginChanges, bool, error) {
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(ctxMetadata, config.PluginUpdateTimeout)
	defer cancel()

	nodePlugins, err := n.NodePlugins(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get plugin list: %w", err)
	}

	tmpDir, err := os.MkdirTemp("/tmp", "jackadi-plugin-tmp")
	if err != nil {
		return nil, false, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			slog.Error("failed to remove plugin tmp directory", "error", err)
		}
	}()

	// if for some reason we failed to resolve the manager address during stream connection, we fallback on the configured manager address
	managerHost := net.JoinHostPort(n.connectedManagerAddr, n.config.PluginServerPort)
	if n.connectedManagerAddr == "" {
		managerHost = net.JoinHostPort(n.config.ManagerAddress, n.config.PluginServerPort)
	}

	upToDate, err1 := n.pluginLoader.DownloadPlugins(nodePlugins, managerHost, n.config.PluginDir, tmpDir)
	slog.Debug("updating plugins")
	changes, changed, err2 := n.pluginLoader.Update(n.config.PluginDir, tmpDir, upToDate)
	return changes, changed, errors.Join(err1, err2)
}

func (n *Node) KillPlugins() {
	mu.Lock()
	n.pluginLoader.KillAll()
	mu.Unlock()
}
