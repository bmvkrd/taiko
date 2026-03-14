package avro

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hamba/avro/v2"
	"github.com/twmb/franz-go/pkg/sr"
)

// SchemaSource configures where to load a schema from.
type SchemaSource struct {
	Subject string // Schema Registry subject name
	Version int    // 0 means "latest"
	File    string // Local .avsc file path (mutually exclusive with Subject)
}

// RegistryConfig holds Schema Registry connection settings.
type RegistryConfig struct {
	URL      string
	Username string
	Password string
}

// ResolveSchema fetches or loads a schema and returns a Serializer ready for encoding.
func ResolveSchema(ctx context.Context, registryCfg *RegistryConfig, source SchemaSource) (*Serializer, error) {
	if source.Subject != "" {
		return resolveFromRegistry(ctx, registryCfg, source)
	}
	if source.File != "" {
		return resolveFromFile(source.File)
	}
	return nil, fmt.Errorf("schema source must specify either 'subject' or 'file'")
}

func resolveFromRegistry(ctx context.Context, cfg *RegistryConfig, source SchemaSource) (*Serializer, error) {
	if cfg == nil || cfg.URL == "" {
		return nil, fmt.Errorf("schema_registry.url is required when using subject-based schemas")
	}

	opts := []sr.ClientOpt{sr.URLs(cfg.URL)}
	if cfg.Username != "" {
		opts = append(opts, sr.BasicAuth(cfg.Username, cfg.Password))
	}

	client, err := sr.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create schema registry client: %w", err)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	version := source.Version
	if version <= 0 {
		version = -1 // franz-go convention for "latest"
	}

	ss, err := client.SchemaByVersion(fetchCtx, source.Subject, version)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch schema for subject %q: %w", source.Subject, err)
	}

	schema, err := avro.Parse(ss.Schema.Schema)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema for subject %q: %w", source.Subject, err)
	}

	return NewSerializer(schema, ss.ID), nil
}

func resolveFromFile(path string) (*Serializer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file %q: %w", path, err)
	}

	schema, err := avro.Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema file %q: %w", path, err)
	}

	// schemaID=0 means no wire format header
	return NewSerializer(schema, 0), nil
}
