package autocompletion

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/node"
	"github.com/kpetremann/jackadi/internal/plugin/inventory"
	"github.com/spf13/cobra"
)

type TaskInfo struct {
	Name    string
	Summary string
}

type PluginInfo struct {
	Name  string
	Tasks map[string]*TaskInfo
}

// GetAvailablePlugins returns all available plugins from the manager's plugins and built-ins.
func GetAvailablePlugins() map[string]*PluginInfo {
	plugins := make(map[string]*PluginInfo)

	builtinPlugins := getBuiltinPlugins()
	maps.Copy(plugins, builtinPlugins)

	externalPlugins := getExternalPlugins()
	maps.Copy(plugins, externalPlugins)

	return plugins
}

// getBuiltinPlugins returns built-in plugins from the registry.
func getBuiltinPlugins() map[string]*PluginInfo {
	plugins := make(map[string]*PluginInfo)

	node.LoadBuiltins(nil)
	pluginNames := inventory.Registry.Names()

	for _, name := range pluginNames {
		coll, err := inventory.Registry.Get(name)
		if err != nil {
			slog.Debug("failed to get plugin from registry", "name", name, "error", err)
			continue
		}

		help, err := coll.Help("")
		if err != nil {
			slog.Debug("failed to get help for plugin", "name", name, "error", err)
			continue
		}

		collInfo := &PluginInfo{
			Name:  name,
			Tasks: make(map[string]*TaskInfo),
		}

		for _, helpText := range help {
			parseHelpOutput(helpText, collInfo)
		}

		plugins[name] = collInfo
	}

	return plugins
}

// getExternalPlugins returns plugins from external plugin files.
func getExternalPlugins() map[string]*PluginInfo {
	pluginDir := config.DefaultPluginDir // TODO: add config file for CLI
	plugins := make(map[string]*PluginInfo)

	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		slog.Debug("plugin directory does not exist", "path", pluginDir)
		return plugins
	}

	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		slog.Debug("failed to read plugin directory", "path", pluginDir, "error", err)
		return plugins
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		pluginPath := filepath.Join(pluginDir, entry.Name())

		// Make sure the file is executable
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&0111 == 0 {
			continue // Not executable
		}

		plugin := getInfoFromPlugin(pluginPath)
		if plugin != nil {
			plugins[plugin.Name] = plugin
		}
	}

	return plugins
}

// getInfoFromPlugin executes a plugin with --describe to get its information.
func getInfoFromPlugin(pluginPath string) *PluginInfo {
	cmd := exec.Command(pluginPath, "--describe")
	cmd.Env = os.Environ()

	output, err := cmd.Output()
	if err != nil {
		slog.Debug("failed to execute plugin describe", "plugin", pluginPath, "error", err)
		return nil
	}

	return parseDescribeOutput(filepath.Base(pluginPath), string(output))
}

// parseDescribeOutput parses the output from plugin --describe command.
func parseDescribeOutput(pluginName string, output string) *PluginInfo {
	info := &PluginInfo{
		Name:  pluginName,
		Tasks: make(map[string]*TaskInfo),
	}

	parseHelpOutput(output, info)
	return info
}

// parseHelpOutput parses help output and populates plugin info.
func parseHelpOutput(output string, collInfo *PluginInfo) {
	lines := strings.SplitSeq(output, "\n")

	for line := range lines {
		originalLine := line
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// format: "  taskname  summary  [flags]"
		if strings.HasPrefix(originalLine, "  ") && !strings.HasPrefix(originalLine, "   ") {
			parseTaskLine(originalLine, collInfo)
		}
	}
}

// parseTaskLine parses a single task line from describe output.
func parseTaskLine(line string, collInfo *PluginInfo) {
	fields := strings.Fields(strings.TrimLeft(line, " "))
	if len(fields) == 0 {
		return
	}
	taskName := fields[0]

	collInfo.Tasks[taskName] = &TaskInfo{
		Name:    taskName,
		Summary: strings.Join(fields, " "),
	}
}

// GetTaskCompletions returns completion suggestions for plugin:task format.
func GetTaskCompletions(toComplete string) ([]string, cobra.ShellCompDirective) {
	plugins := GetAvailablePlugins()
	completions := []string{}

	// complete task
	if strings.Contains(toComplete, config.PluginSeparator) {
		parts := strings.SplitN(toComplete, config.PluginSeparator, 2)
		if len(parts) != 2 {
			return completions, cobra.ShellCompDirectiveDefault
		}

		pluginName := parts[0]
		taskPrefix := parts[1]

		plugin, exists := plugins[pluginName]
		if !exists {
			return completions, cobra.ShellCompDirectiveDefault
		}

		for taskName, taskInfo := range plugin.Tasks {
			if !strings.HasPrefix(taskName, taskPrefix) {
				continue
			}
			completion := fmt.Sprintf("%s:%s", pluginName, taskName)
			if taskInfo.Summary != "" {
				completion = fmt.Sprintf("%s\t%s", completion, taskInfo.Summary)
			}
			completions = append(completions, completion)
		}
		return completions, cobra.ShellCompDirectiveDefault
	}

	// complete plugin only
	for p := range plugins {
		if strings.HasPrefix(p, toComplete) {
			completions = append(completions, p+config.PluginSeparator)
		}
	}

	return completions, cobra.ShellCompDirectiveDefault
}
