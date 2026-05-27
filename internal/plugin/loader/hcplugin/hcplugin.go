package hcplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"github.com/kpetremann/jackadi/internal/plugin/core"
	"github.com/kpetremann/jackadi/internal/plugin/inventory"
	"github.com/kpetremann/jackadi/internal/plugin/types"
)

var PluginMap = map[string]goplugin.Plugin{
	"plugin": &core.HCPlugin{},
}

type PluginInfo struct {
	name    string
	file    string
	version string
	config  goplugin.ClientConfig
	client  *goplugin.Client
}

type Loader struct {
	logger  hclog.Logger
	plugins map[string]PluginInfo // key=filepath
}

// discover all non .so plugins in plugins/
//
// This is only for hashicorp/go-plugin plugins.
func discover(pluginDir string) []string {
	files, err := os.ReadDir(pluginDir)
	if err != nil {
		slog.Warn("no HC plugin loaded")
		return []string{}
	}

	pluginFiles := []string{}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".so") {
			pluginFiles = append(pluginFiles, file.Name())
		}
	}

	return pluginFiles
}

func CalculateChecksum(file string) (string, error) {
	fd, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer fd.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, fd); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func New() Loader {
	return Loader{
		plugins: make(map[string]PluginInfo),
		logger: hclog.FromStandardLogger(log.Default(), &hclog.LoggerOptions{
			Name:   "plugin",
			Output: os.Stdout,
			Level:  hclog.Debug,
		}),
	}
}

// Load and register a plugin.
func (l *Loader) load(path string) error {
	checksum, err := CalculateChecksum(path)
	if err != nil {
		return fmt.Errorf("plugin checksum failed: %w", err)
	}

	cfg := goplugin.ClientConfig{
		HandshakeConfig:  core.Handshake,
		Plugins:          PluginMap,
		Cmd:              exec.Command(path),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolNetRPC, goplugin.ProtocolGRPC},
		Logger:           l.logger,
	}

	client := goplugin.NewClient(&cfg)
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return fmt.Errorf("plugin client error: %w", err)
	}

	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		client.Kill()
		return fmt.Errorf("plugin dispense error: %w", err)
	}

	coll, ok := raw.(core.Plugin)
	if !ok {
		client.Kill()

		methods := describeInterface(raw, false)
		expectedMethods := describeInterface((*core.Plugin)(nil), true)

		return fmt.Errorf(
			"plugin not implementing the Plugin interface: methods=%v, expected_methods=%v",
			methods, expectedMethods,
		)
	}

	name, err := coll.Name()
	if err != nil || name == "" {
		client.Kill()
		return fmt.Errorf("bad plugin name: %w", err)
	}

	// register the loaded plugin publicly
	if err := inventory.Registry.Register(coll); err != nil {
		client.Kill()
		return fmt.Errorf("failed to register hcplugin: %w", err)
	}

	// store the plugin internally for management (kill, reload etc...)
	l.plugins[path] = PluginInfo{name: name, file: filepath.Base(path), version: checksum, config: cfg, client: client}

	return nil
}

func describeInterface(raw any, unreference bool) []string {
	t := reflect.TypeOf(raw)

	if unreference && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	methods := make([]string, 0, t.NumMethod())

	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		methods = append(methods, fmt.Sprintf("%s : %s", m.Name, m.Type))
	}

	return methods
}

// Load loads all plugins.
func (l *Loader) Load(pluginDir string) {
	plugins := discover(pluginDir)
	for _, file := range plugins {
		path := filepath.Join(pluginDir, file)
		if err := l.load(path); err != nil {
			slog.Error("failed to load plugin", "error", err, "plugin", file)
		}
	}
}

// download the plugin from the provided URL.
// The name is only the identifier of the plugin.
func download(name, url, tmpDir string) error {
	client := http.Client{Timeout: time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("'%s': %w", url, err)
	}
	if resp == nil {
		return fmt.Errorf("'%s': received nil response", url)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("'%s' http error %d", url, resp.StatusCode)
	}

	localPath := filepath.Join(tmpDir, name)
	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create/access '%s' plugin file: %w", name, err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write '%s' plugin file : %w", name, err)
	}

	if err := os.Chmod(localPath, 0755); err != nil {
		return fmt.Errorf("chmod 644 failed for '%s' plugin file : %w", name, err)
	}
	return nil
}

// DownloadPlugins downloads plugins from the manager, excluding already up to date plugins (same checksum).
func (l *Loader) DownloadPlugins(nodePlugins map[string]string, managerHost, pluginDir, tmpDir string) ([]string, error) {
	slog.Debug("sync", "values", nodePlugins)
	upToDate := []string{}
	var errs error
	for file, checksum := range nodePlugins {
		path := filepath.Join(pluginDir, file)
		if p, ok := l.plugins[path]; ok && p.version == checksum {
			slog.Debug("plugin not downloaded", "reason", "already up to date", "plugin_file", file)
			upToDate = append(upToDate, p.file)
			continue
		}

		url := fmt.Sprintf("http://%s/plugin/%s", managerHost, file)
		slog.Debug("starting plugin sync", "plugin_file", file, "url", url)
		if err := download(file, url, tmpDir); err != nil {
			errs = errors.Join(errs, fmt.Errorf("new plugin not installed: '%s' file not downloaded: %w", file, err))
			slog.Error("failed to download", "plugin_file", file, "url", url, "error", err)
			continue
		}
		slog.Debug("plugin downloaded", "plugin_file", file, "url", url)
	}

	return upToDate, errs
}

func (l *Loader) Update(pluginDir, tmpDir string, upToDate []string) ([]types.PluginChanges, bool, error) {
	pluginsFile := discover(tmpDir)
	newPluginNameList := []string{}
	changes := []types.PluginChanges{}

	var errs error
	changed := false

	for _, file := range pluginsFile {
		candidatePluginPath := filepath.Join(tmpDir, file)
		path := filepath.Join(pluginDir, file)

		slog.Debug("syncing plugin", "plugin_file", file)

		// handle new plugin
		p, ok := l.plugins[path]
		if !ok {
			slog.Debug("new plugin to install", "plugin_file", file)
			if err := os.Rename(candidatePluginPath, path); err != nil {
				// TODO: the error is not sent back to the user CLI
				slog.Error("new plugin not installed: failed to replace plugin file", "error", err, "plugin_file", file)
				errs = errors.Join(errs, fmt.Errorf("new plugin not installed: failed to replace '%s' file: %w", file, err))
				continue
			}

			if err := l.load(path); err != nil {
				slog.Error("plugin not loaded", "error", err, "plugin_file", file)
				errs = errors.Join(errs, fmt.Errorf("new plugin not installed: '%s' file not loaded: %w", file, err))
				continue
			}

			newPluginNameList = append(newPluginNameList, l.plugins[path].name)
			changes = append(changes, types.PluginChanges{Name: l.plugins[path].name, FileName: file, New: true})
			changed = true
			continue
		}

		// reload outdated plugin
		slog.Debug("reloading updated plugin", "plugin_file", file)
		p.client.Kill()
		if err := inventory.Registry.Unregister(p.name); err != nil {
			slog.Error("failed to unload plugin", "error", err, "plugin_file", file)
			errs = errors.Join(errs, fmt.Errorf("plugin update failed: unable to unregister existing '%s' file: %w", file, err))
		}

		if err := os.Rename(candidatePluginPath, path); err != nil {
			slog.Error("plugin update failed: failed to replace plugin file", "error", err, "plugin_file", file)
			errs = errors.Join(errs, fmt.Errorf("plugin update failed: failed to replace '%s' file: %w", file, err))
			continue
		}

		if err := l.load(path); err != nil {
			slog.Error("failed to reload plugin", "error", err, "plugin_file", file)
			errs = errors.Join(errs, fmt.Errorf("plugin update failed: failed to reload '%s' file: %w", file, err))
			continue
		}
		newPluginNameList = append(newPluginNameList, p.name)
		changes = append(changes, types.PluginChanges{Name: p.name, FileName: file, Updated: true})
		changed = true
		slog.Info("plugin loaded", "name", p.name)
	}

	// unload plugins which should be removed
	slog.Debug("new list of plugins", "list", newPluginNameList)
	for _, p := range l.plugins {
		// ignore up to date plugin
		if slices.Contains(upToDate, p.file) {
			slog.Debug("plugin checksum unchanged", "plugin_file", p.file)
			newPluginNameList = append(newPluginNameList, p.name)
			changes = append(changes, types.PluginChanges{Name: p.name, FileName: p.file})
			continue
		}

		if !slices.Contains(newPluginNameList, p.name) {
			slog.Debug("removing plugin", "name", p.name)
			if err := inventory.Registry.Unregister(p.name); err != nil {
				slog.Error("failed to unload plugin", "error", err, "plugin_name", p.name)
				errs = errors.Join(errs, fmt.Errorf("plugin removal failed: failed to unload '%s' plugin: %w", p.name, err))
				continue
			}

			if err := os.Remove(p.file); err != nil {
				slog.Error("plugin removal partially failed: failed to delete file", "error", err, "plugin_file", p.file)
				errs = errors.Join(errs, fmt.Errorf("plugin removal partially failed: failed to delete '%s' file: %w", p.file, err))
			}

			l.plugins[p.name].client.Kill()
			changes = append(changes, types.PluginChanges{Name: p.name, Deleted: true})
			changed = true
			delete(l.plugins, p.name)
			slog.Debug("plugin removed", "name", p.name)
		}
	}

	return changes, changed, errs
}

func (l *Loader) Kill(name string) {
	if p, ok := l.plugins[name]; ok {
		p.client.Kill()
	}
}

func (l *Loader) KillAll() {
	for _, p := range l.plugins {
		p.client.Kill()
	}
}
