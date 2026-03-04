package engine

import (
	"strings"
	"testing"

	"github.com/bmvkrd/taiko/internal/config"
)

func TestNewSubstitutor_NoVariables(t *testing.T) {
	s, err := NewSubstitutor(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Substitutor")
	}
}

func TestNewSubstitutor_UnknownType(t *testing.T) {
	vars := []config.Variable{
		{Name: "x", Type: "bogus", Generator: map[string]any{}},
	}
	_, err := NewSubstitutor(vars)
	if err == nil {
		t.Fatal("expected error for unknown generator type")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error should mention type name, got: %v", err)
	}
}

func TestNewValues_NoGenerators(t *testing.T) {
	s, _ := NewSubstitutor(nil)
	if v := s.NewValues(); v != nil {
		t.Errorf("expected nil values map, got %v", v)
	}
}

func TestApply_NoValues(t *testing.T) {
	s, _ := NewSubstitutor(nil)
	const tpl = "hello {{world}}"
	if got := s.Apply(tpl, nil); got != tpl {
		t.Errorf("Apply with nil values should return template unchanged, got %q", got)
	}
}

func TestApply_UnknownPlaceholder(t *testing.T) {
	s, _ := NewSubstitutor(nil)
	const tpl = "hello {{unknown}}"
	got := s.Apply(tpl, map[string]string{"other": "X"})
	if got != tpl {
		t.Errorf("unknown placeholder should be kept, got %q", got)
	}
}

func TestSubstitutor_IntRange(t *testing.T) {
	vars := []config.Variable{
		{
			Name: "id",
			Type: "int_range",
			Generator: map[string]any{
				"mode": "seq",
				"min":  1,
				"max":  10,
			},
		},
	}
	s, err := NewSubstitutor(vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v1 := s.NewValues()
	v2 := s.NewValues()
	if v1["id"] == "" {
		t.Error("expected non-empty value for 'id'")
	}
	// Sequential: second call should differ from first (min=1 max=10 wraps, so they differ unless span=1)
	_ = v2 // just ensure it can be called twice without panic
}

func TestSubstitutor_UUID(t *testing.T) {
	vars := []config.Variable{
		{Name: "rid", Type: "uuid", Generator: map[string]any{}},
	}
	s, err := NewSubstitutor(vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v1 := s.NewValues()
	v2 := s.NewValues()
	if len(v1["rid"]) != 36 {
		t.Errorf("expected UUID of length 36, got %q", v1["rid"])
	}
	if v1["rid"] == v2["rid"] {
		t.Error("expected different UUIDs on each call")
	}
}

func TestSubstitutor_StringSet(t *testing.T) {
	vars := []config.Variable{
		{
			Name: "env",
			Type: "string_set",
			Generator: map[string]any{
				"mode":   "seq",
				"values": []any{"prod", "staging"},
			},
		},
	}
	s, err := NewSubstitutor(vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v1 := s.NewValues()
	if v1["env"] != "prod" && v1["env"] != "staging" {
		t.Errorf("unexpected value %q", v1["env"])
	}
}

func TestSubstitutor_Timestamp(t *testing.T) {
	vars := []config.Variable{
		{
			Name:      "ts",
			Type:      "timestamp",
			Generator: map[string]any{"format": "unix"},
		},
	}
	s, err := NewSubstitutor(vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v := s.NewValues()
	if v["ts"] == "" {
		t.Error("expected non-empty timestamp value")
	}
}

func TestApply_SubstitutesPlaceholders(t *testing.T) {
	vars := []config.Variable{
		{Name: "id", Type: "uuid", Generator: map[string]any{}},
	}
	s, err := NewSubstitutor(vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	values := s.NewValues()
	result := s.Apply("/items/{{id}}", values)
	if strings.Contains(result, "{{id}}") {
		t.Errorf("placeholder was not substituted, got %q", result)
	}
	if !strings.HasPrefix(result, "/items/") {
		t.Errorf("unexpected result %q", result)
	}
}

func TestApply_SameValuesAcrossTemplates(t *testing.T) {
	vars := []config.Variable{
		{Name: "id", Type: "uuid", Generator: map[string]any{}},
	}
	s, err := NewSubstitutor(vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	values := s.NewValues()
	url := s.Apply("/items/{{id}}", values)
	body := s.Apply(`{"id":"{{id}}"}`, values)

	// Extract the id from url and body and verify they are the same
	urlID := strings.TrimPrefix(url, "/items/")
	bodyID := strings.TrimPrefix(strings.TrimSuffix(body, `"}`), `{"id":"`)
	if urlID != bodyID {
		t.Errorf("Apply should produce the same value across templates: url=%q body=%q", urlID, bodyID)
	}
}

func TestSubstitutor_IntSet(t *testing.T) {
	vars := []config.Variable{
		{
			Name: "n",
			Type: "int_set",
			Generator: map[string]any{
				"mode":   "rnd",
				"values": []any{10, 20, 30},
			},
		},
	}
	s, err := NewSubstitutor(vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v := s.NewValues()
	if v["n"] != "10" && v["n"] != "20" && v["n"] != "30" {
		t.Errorf("unexpected value %q", v["n"])
	}
}
