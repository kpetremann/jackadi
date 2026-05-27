package inventory

import (
	"fmt"
	"sync"

	"github.com/kpetremann/jackadi/internal/plugin/core"
)

var Registry = New()

type registry struct {
	plugins map[string]core.Plugin
	lock    *sync.Mutex
}

func New() registry {
	return registry{
		plugins: make(map[string]core.Plugin),
		lock:    &sync.Mutex{},
	}
}

func (r *registry) Get(name string) (core.Plugin, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if m, ok := r.plugins[name]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("'%s' not registered", name)
}

func (r *registry) Register(m core.Plugin) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	name, err := m.Name()
	if err != nil {
		return err
	}
	if _, exists := r.plugins[name]; exists {
		return fmt.Errorf("%s already exists", name)
	}

	r.plugins[name] = m
	return nil
}

func (r *registry) Unregister(name string) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	if _, exists := r.plugins[name]; !exists {
		return fmt.Errorf("%s does not exist", name)
	}

	delete(r.plugins, name)
	return nil
}

func (r *registry) Names() []string {
	r.lock.Lock()
	defer r.lock.Unlock()

	tasks := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		tasks = append(tasks, name)
	}

	return tasks
}
