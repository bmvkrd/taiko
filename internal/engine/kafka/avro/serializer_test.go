package avro

import (
	"encoding/binary"
	"testing"

	"github.com/hamba/avro/v2"
)

func testSchema(t *testing.T) avro.Schema {
	t.Helper()
	s, err := avro.Parse(`{
		"type": "record",
		"name": "TestEvent",
		"fields": [
			{"name": "id", "type": "string"},
			{"name": "count", "type": "int"}
		]
	}`)
	if err != nil {
		t.Fatalf("failed to parse test schema: %v", err)
	}
	return s
}

func TestEncode_SimpleRecord(t *testing.T) {
	s := NewSerializer(testSchema(t), 0)

	encoded, err := s.Encode(`{"id": "abc", "count": 42}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("expected non-empty output")
	}
}

func TestEncode_WithWireFormat(t *testing.T) {
	schemaID := 123
	s := NewSerializer(testSchema(t), schemaID)

	encoded, err := s.Encode(`{"id": "abc", "count": 42}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if encoded[0] != 0x00 {
		t.Fatalf("expected magic byte 0x00, got 0x%02x", encoded[0])
	}
	gotID := binary.BigEndian.Uint32(encoded[1:5])
	if gotID != uint32(schemaID) {
		t.Fatalf("expected schema ID %d, got %d", schemaID, gotID)
	}
	if len(encoded) <= 5 {
		t.Fatal("expected avro payload after wire format header")
	}
}

func TestEncode_WithoutWireFormat(t *testing.T) {
	s := NewSerializer(testSchema(t), 0)

	encoded, err := s.Encode(`{"id": "abc", "count": 42}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without wire format, first byte should NOT be 0x00 magic byte
	// (unless the Avro data happens to start with 0x00, but for a string "abc"
	// the first byte is a varint length which is 0x06).
	if len(encoded) >= 5 {
		possibleID := binary.BigEndian.Uint32(encoded[1:5])
		if encoded[0] == 0x00 && possibleID == 0 {
			// This would be suspicious but schema ID 0 means no wire format,
			// so we just check size difference
		}
	}
	// Encoding without wire format should produce fewer bytes than with
	sWire := NewSerializer(testSchema(t), 1)
	encodedWire, _ := sWire.Encode(`{"id": "abc", "count": 42}`)
	if len(encodedWire) != len(encoded)+5 {
		t.Fatalf("expected wire format to add exactly 5 bytes, got %d vs %d", len(encodedWire), len(encoded))
	}
}

func TestEncode_InvalidJSON(t *testing.T) {
	s := NewSerializer(testSchema(t), 0)

	_, err := s.Encode(`not valid json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestEncode_SchemaViolation(t *testing.T) {
	s := NewSerializer(testSchema(t), 0)

	// "count" should be int, not string
	_, err := s.Encode(`{"id": "abc", "count": "not-an-int"}`)
	if err == nil {
		t.Fatal("expected error for schema violation")
	}
}
