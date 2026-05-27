package node

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/claytonsingh/golib/dotaccess"
	"github.com/kpetremann/jackadi/internal/config"
	"github.com/kpetremann/jackadi/internal/plugin/inventory"
	"github.com/kpetremann/jackadi/internal/serializer"
	"github.com/kpetremann/jackadi/sdk"
)

func NewSpecsManager() (*SpecsManager, error) {
	s := &SpecsManager{
		mutex: &sync.RWMutex{},
		specs: make(map[string]any),
	}

	c := sdk.New(config.SpecManagerPrefix)
	c.MustRegisterTask("list", s.List)
	c.MustRegisterTask("all", s.All)
	c.MustRegisterTask("get", s.Get)

	if err := inventory.Registry.Register(c); err != nil {
		return nil, fmt.Errorf("cannot register as a plugin: %w", err)
	}

	return s, nil
}

type SpecsManager struct {
	specs map[string]any
	mutex *sync.RWMutex
}

func (s *SpecsManager) List() ([]string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return slices.Sorted(maps.Keys(s.specs)), nil
}

func (s *SpecsManager) All() (map[string]any, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	out := maps.Clone(s.specs)
	return out, nil
}

func (s *SpecsManager) Get(spec string) (any, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	a, err := dotaccess.NewAccessorDot[any, map[string]any](&s.specs, spec)
	if err != nil {
		return nil, err
	}

	return a.Get(), nil
}

func (s *SpecsManager) StartSpecCollector(ctx context.Context, syncReq chan struct{}) {
	defer slog.Info("spec collector closed")

	t := time.NewTicker(config.SpecCollectionInterval)
	for {
		slog.Debug("collecting specs")
		plugins := inventory.Registry.Names()
		newSpecs := make(map[string]any)

		for _, name := range plugins {
			c, err := inventory.Registry.Get(name)
			if err != nil {
				slog.Error("failed to get specs tasks", "plugin", name, "error", err)
				continue
			}
			out, err := c.CollectSpecs(ctx)
			if err != nil {
				slog.Error("failed to fetch specs", "plugin", name, "error", err)
				continue
			}

			var specs map[string]any
			if err := serializer.JSON.Unmarshal(out, &specs); err != nil {
				slog.Error("failed to unmarshal specs", "plugin", name, "error", err)
				continue
			}

			if len(specs) > 0 {
				newSpecs[name] = specs
			}
		}

		s.mutex.Lock()
		s.specs = newSpecs
		s.mutex.Unlock()

		slog.Debug("specs collected", "count", len(s.specs))
		select {
		case <-t.C:
		case <-syncReq:
		case <-ctx.Done():
			return
		}
	}
}
