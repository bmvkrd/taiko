package metrics

import (
	"fmt"
)

// Factory is a function that creates a new connector instance
type Factory func() Connector

// registry holds all available connectors
var registry = map[string]Factory{
	"console":    func() Connector { return NewConsoleConnector() },
	"prometheus": func() Connector { return NewPrometheusConnector() },
	"s3":         func() Connector { return NewS3Connector() },
}

// Get creates a connector instance by name
func Get(name string) (Connector, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown connector: %s (available: console, prometheus, s3)", name)
	}
	return factory(), nil
}

// Register adds a new connector to the registry (for custom connectors)
func Register(name string, factory Factory) {
	registry[name] = factory
}

// Available returns a list of available connector names
func Available() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
