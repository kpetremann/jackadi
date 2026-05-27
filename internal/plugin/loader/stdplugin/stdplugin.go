package stdplugin

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	stdplugin "plugin"
	"strings"

	"github.com/kpetremann/jackadi/internal/plugin/core"
	"github.com/kpetremann/jackadi/internal/plugin/inventory"
)

func loadPlugin(pluginDir, file string) (core.Plugin, error) {
	f := filepath.Join(pluginDir, file)

	ext, err := stdplugin.Open(f)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin: %w", err)
	}

	modNew, err := ext.Lookup("New")
	if err != nil {
		return nil, fmt.Errorf("missing 'New' symbol in plugin: %w", err)
	}

	module, ok := modNew.(func() core.Plugin)
	if !ok {
		return nil, fmt.Errorf("unexpected type from 'New' module symbol")
	}

	return module(), nil
}

// Load loads std/plugins.
//
// Its usage is not recommended because of std/plugin limitation.
// See the warning in the documentation for details: https://pkg.go.dev/plugin
func Load(pluginDir string) {
	files, err := os.ReadDir(pluginDir)
	if err != nil {
		slog.Warn("no plugin loaded")
		return
	}

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".so") {
			continue
		}

		t, err := loadPlugin(pluginDir, file.Name())
		if err != nil {
			slog.Error("plugin not loaded", "error", err, "file", file.Name())
			continue
		}

		if err := inventory.Registry.Register(t); err != nil {
			name, _ := t.Name()
			slog.Error("plugin not loaded", "error", err, "task", name)
			continue
		}
	}
}
