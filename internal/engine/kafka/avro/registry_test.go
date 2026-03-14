package avro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFromFile(t *testing.T) {
	path := filepath.Join("testdata", "test_event.avsc")

	s, err := ResolveSchema(context.Background(), nil, SchemaSource{File: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the serializer works
	encoded, err := s.Encode(`{"id": "test", "count": 1}`)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("expected non-empty output")
	}

	// File-based schema should not use wire format (no 5-byte header)
	if s.useWireFormat {
		t.Fatal("expected no wire format for file-based schema")
	}
}

func TestResolveFromFile_NotExists(t *testing.T) {
	_, err := ResolveSchema(context.Background(), nil, SchemaSource{File: "testdata/nonexistent.avsc"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestResolveFromFile_InvalidSchema(t *testing.T) {
	tmpDir := t.TempDir()
	badSchema := filepath.Join(tmpDir, "bad.avsc")
	os.WriteFile(badSchema, []byte(`{"type": "invalid_type"}`), 0644)

	_, err := ResolveSchema(context.Background(), nil, SchemaSource{File: badSchema})
	if err == nil {
		t.Fatal("expected error for invalid schema")
	}
}

func TestResolveSchema_NoSourceSpecified(t *testing.T) {
	_, err := ResolveSchema(context.Background(), nil, SchemaSource{})
	if err == nil {
		t.Fatal("expected error when neither subject nor file is specified")
	}
}

func TestResolveFromRegistry(t *testing.T) {
	schemaJSON := `{
		"type": "record",
		"name": "TestEvent",
		"fields": [
			{"name": "id", "type": "string"},
			{"name": "count", "type": "int"}
		]
	}`

	// Mock Schema Registry server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/subjects/test-events-value/versions/latest"
		if r.URL.Path != expectedPath {
			http.Error(w, fmt.Sprintf("unexpected path: %s (expected %s)", r.URL.Path, expectedPath), http.StatusNotFound)
			return
		}
		resp := map[string]interface{}{
			"subject":    "test-events-value",
			"version":    1,
			"id":         42,
			"schema":     schemaJSON,
			"schemaType": "AVRO",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	regCfg := &RegistryConfig{URL: server.URL}
	s, err := ResolveSchema(context.Background(), regCfg, SchemaSource{
		Subject: "test-events-value",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Schema from registry should use wire format
	if !s.useWireFormat {
		t.Fatal("expected wire format for registry-based schema")
	}
	if s.schemaID != 42 {
		t.Fatalf("expected schema ID 42, got %d", s.schemaID)
	}

	// Verify encoding works
	encoded, err := s.Encode(`{"id": "test", "count": 1}`)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if encoded[0] != 0x00 {
		t.Fatalf("expected magic byte 0x00, got 0x%02x", encoded[0])
	}
}

func TestResolveFromRegistry_MissingURL(t *testing.T) {
	_, err := ResolveSchema(context.Background(), nil, SchemaSource{Subject: "test"})
	if err == nil {
		t.Fatal("expected error when registry config is nil")
	}

	_, err = ResolveSchema(context.Background(), &RegistryConfig{}, SchemaSource{Subject: "test"})
	if err == nil {
		t.Fatal("expected error when registry URL is empty")
	}
}

func TestResolveFromRegistry_WithBasicAuth(t *testing.T) {
	schemaJSON := `{
		"type": "record",
		"name": "TestEvent",
		"fields": [
			{"name": "id", "type": "string"},
			{"name": "count", "type": "int"}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "myuser" || pass != "mypass" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		resp := map[string]interface{}{
			"subject":    "test-events-value",
			"version":    1,
			"id":         99,
			"schema":     schemaJSON,
			"schemaType": "AVRO",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	regCfg := &RegistryConfig{
		URL:      server.URL,
		Username: "myuser",
		Password: "mypass",
	}
	s, err := ResolveSchema(context.Background(), regCfg, SchemaSource{
		Subject: "test-events-value",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.schemaID != 99 {
		t.Fatalf("expected schema ID 99, got %d", s.schemaID)
	}
}
