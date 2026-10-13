package runtimes

import (
	"errors"
	"sort"
	"sync"
)

type MemoryRegistry struct {
	mu       sync.RWMutex
	runtimes map[string]Runtime
}

func NewRegistry() *MemoryRegistry {
	return &MemoryRegistry{runtimes: map[string]Runtime{}}
}

func NewDefaultRegistry() *MemoryRegistry {
	registry := NewRegistry()
	defaults := []Runtime{
		NewStaticRuntime(),
		NewPHPRuntime(),
		NewPythonRuntime(),
		NewGoRuntime(),
		NewNodeRuntime(),
	}
	for _, runtime := range defaults {
		if err := registry.Register(runtime); err != nil {
			panic(err)
		}
	}
	return registry
}

func (r *MemoryRegistry) Register(runtime Runtime) error {
	if runtime == nil || runtime.Name() == "" {
		return errors.New("runtime name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runtimes[runtime.Name()]; exists {
		return errors.New("runtime already registered")
	}
	r.runtimes[runtime.Name()] = runtime
	return nil
}

func (r *MemoryRegistry) Get(name string) (Runtime, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runtime, ok := r.runtimes[name]
	return runtime, ok
}

func (r *MemoryRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]string, 0, len(r.runtimes))
	for name := range r.runtimes {
		items = append(items, name)
	}
	sort.Strings(items)
	return items
}
