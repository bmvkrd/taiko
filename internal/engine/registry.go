package engine

import (
	"fmt"
	"sort"

	"github.com/bmvkrd/taiko/internal/config"
)

// Factory creates a new engine instance from configuration
type Factory func(cfg *config.Config) (Engine, error)

// registry holds all registered engine factories
var registry = map[string]Factory{}

// Register adds an engine factory to the registry
func Register(name string, factory Factory) {
	registry[name] = factory
}

// Get retrieves and instantiates an engine from config
func Get(cfg *config.Config) (Engine, error) {
	factory, ok := registry[cfg.Engine]
	if !ok {
		return nil, fmt.Errorf("unknown engine type: %s (available: %v)", cfg.Engine, Available())
	}
	return factory(cfg)
}

// Available returns a list of all registered engine names
func Available() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
