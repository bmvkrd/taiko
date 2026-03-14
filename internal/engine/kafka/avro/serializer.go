package avro

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/hamba/avro/v2"
)

// Serializer encodes JSON strings into Avro binary, optionally prefixed with
// the Confluent wire format header (magic byte 0x00 + 4-byte schema ID).
// It is safe for concurrent use.
type Serializer struct {
	schema        avro.Schema
	schemaID      int
	useWireFormat bool
}

// NewSerializer creates a serializer from a parsed Avro schema. If schemaID > 0,
// the Confluent wire format header is prepended to every encoded message.
func NewSerializer(schema avro.Schema, schemaID int) *Serializer {
	return &Serializer{
		schema:        schema,
		schemaID:      schemaID,
		useWireFormat: schemaID > 0,
	}
}

// Encode takes a JSON string (after variable substitution), converts it to
// an Avro binary payload, and optionally prepends the Confluent wire header.
func (s *Serializer) Encode(jsonStr string) ([]byte, error) {
	var data any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, fmt.Errorf("avro: invalid JSON: %w", err)
	}

	// json.Unmarshal decodes all numbers as float64. Avro requires exact Go
	// types (int for Avro int/long, float32 for float, float64 for double).
	// Walk the schema to coerce float64 → int where the schema expects it.
	data = coerceTypes(data, s.schema)

	avroBin, err := avro.Marshal(s.schema, data)
	if err != nil {
		return nil, fmt.Errorf("avro: encoding failed: %w", err)
	}

	if !s.useWireFormat {
		return avroBin, nil
	}

	// Confluent wire format: [0x00] [4-byte schema ID big-endian] [avro binary]
	buf := make([]byte, 5+len(avroBin))
	buf[0] = 0x00
	binary.BigEndian.PutUint32(buf[1:5], uint32(s.schemaID))
	copy(buf[5:], avroBin)
	return buf, nil
}

// coerceTypes walks the JSON-unmarshaled data and converts float64 values to
// the Go types that hamba/avro expects, based on the Avro schema.
func coerceTypes(data any, schema avro.Schema) any {
	switch s := schema.(type) {
	case *avro.RecordSchema:
		m, ok := data.(map[string]any)
		if !ok {
			return data
		}
		for _, field := range s.Fields() {
			if v, exists := m[field.Name()]; exists {
				m[field.Name()] = coerceTypes(v, field.Type())
			}
		}
		return m

	case *avro.PrimitiveSchema:
		f, ok := data.(float64)
		if !ok {
			return data
		}
		switch s.Type() {
		case avro.Int:
			return int(f)
		case avro.Long:
			return int64(f)
		case avro.Float:
			return float32(f)
		}
		return data

	case *avro.ArraySchema:
		arr, ok := data.([]any)
		if !ok {
			return data
		}
		for i, item := range arr {
			arr[i] = coerceTypes(item, s.Items())
		}
		return arr

	case *avro.MapSchema:
		m, ok := data.(map[string]any)
		if !ok {
			return data
		}
		for k, v := range m {
			m[k] = coerceTypes(v, s.Values())
		}
		return m

	case *avro.UnionSchema:
		// Try each type in the union until one succeeds.
		// For null unions (common pattern ["null", "type"]), skip null.
		for _, t := range s.Types() {
			if t.Type() == avro.Null {
				continue
			}
			return coerceTypes(data, t)
		}
		return data
	}

	return data
}
