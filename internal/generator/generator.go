package generator

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	mathrand "math/rand/v2"
	"sync/atomic"
)

type Generator interface {
	Next() any // returns typed value
	GetGeneratorType() GeneratorType
}

type GeneratorType string

const (
	IntSetType    GeneratorType = "int_set"
	IntRangeType  GeneratorType = "int_range"
	UUIDType      GeneratorType = "uuid"
	StringSetType GeneratorType = "string_set"
)

type Mode string

const (
	SeqMode Mode = "seq"
	RndMode Mode = "rnd"
)

// IntRangeGenerator generates integers from min to max.
// In "seq" mode, values are generated sequentially and wrap around.
// In "rnd" mode, values are generated randomly within the range.
// Thread-safe for concurrent access from multiple workers.
type IntRangeGenerator struct {
	min     int
	max     int
	span    int64 // max - min + 1, precomputed for efficiency
	mode    Mode
	current atomic.Int64
}

// NewIntRangeGenerator creates a new IntRangeGenerator that generates integers
// from min to max (inclusive). In "seq" mode, values wrap back to min after
// reaching max. In "rnd" mode, values are randomly selected from the range.
func NewIntRangeGenerator(min, max int, mode Mode) (*IntRangeGenerator, error) {
	if min > max {
		return nil, errors.New("min must be less than or equal to max")
	}
	if mode != SeqMode && mode != RndMode {
		return nil, errors.New("mode must be 'seq' or 'rnd'")
	}
	return &IntRangeGenerator{
		min:  min,
		max:  max,
		span: int64(max - min + 1),
		mode: mode,
	}, nil
}

// Next returns the next integer in the range.
// In "seq" mode, values cycle from min to max, then wrap back to min.
// In "rnd" mode, a random value within the range is returned.
func (g *IntRangeGenerator) Next() any {
	if g.mode == RndMode {
		return g.min + mathrand.IntN(int(g.span))
	}
	counter := g.current.Add(1) - 1 // get current value before increment
	offset := counter % g.span
	return g.min + int(offset)
}

// GetGeneratorType returns RangeType.
func (g *IntRangeGenerator) GetGeneratorType() GeneratorType {
	return IntRangeType
}

// IntSetGenerator generates integers from a set of values.
// In "seq" mode, values are generated sequentially and wrap around.
// In "rnd" mode, values are randomly selected from the set.
// Thread-safe for concurrent access from multiple workers.
type IntSetGenerator struct {
	values  []int
	mode    Mode
	current atomic.Int64
}

// NewIntSetGenerator creates a new IntSetGenerator that generates values from
// the provided set. In "seq" mode, values cycle sequentially, wrapping back to
// the beginning after the last value. In "rnd" mode, values are randomly selected.
func NewIntSetGenerator(values []int, mode Mode) (*IntSetGenerator, error) {
	if len(values) == 0 {
		return nil, errors.New("values slice must not be empty")
	}
	if mode != SeqMode && mode != RndMode {
		return nil, errors.New("mode must be 'seq' or 'rnd'")
	}
	// Make a copy to prevent external modification
	valuesCopy := make([]int, len(values))
	copy(valuesCopy, values)
	return &IntSetGenerator{
		values: valuesCopy,
		mode:   mode,
	}, nil
}

// Next returns the next integer from the set.
// In "seq" mode, values cycle through the set sequentially, then wrap back.
// In "rnd" mode, a random value from the set is returned.
func (g *IntSetGenerator) Next() any {
	if g.mode == RndMode {
		return g.values[mathrand.IntN(len(g.values))]
	}
	counter := g.current.Add(1) - 1 // get current value before increment
	index := counter % int64(len(g.values))
	return g.values[index]
}

// GetGeneratorType returns SetType.
func (g *IntSetGenerator) GetGeneratorType() GeneratorType {
	return IntSetType
}

// UUIDGenerator generates random UUIDs (version 4).
// Each call to Next() returns a new UUID string.
// Thread-safe for concurrent access from multiple workers.
type UUIDGenerator struct{}

// NewUUIDGenerator creates a new UUIDGenerator.
func NewUUIDGenerator() *UUIDGenerator {
	return &UUIDGenerator{}
}

// Next returns a new random UUID (version 4) as a string.
func (g *UUIDGenerator) Next() any {
	var uuid [16]byte
	_, _ = rand.Read(uuid[:])
	// Set version 4 (random) bits
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	// Set variant bits (RFC 4122)
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	var buf [36]byte
	hex.Encode(buf[0:8], uuid[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], uuid[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], uuid[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], uuid[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], uuid[10:16])

	return string(buf[:])
}

// GetGeneratorType returns UUIDType.
func (g *UUIDGenerator) GetGeneratorType() GeneratorType {
	return UUIDType
}

// StringSetGenerator generates strings from a set of values.
// In "seq" mode, values are generated sequentially and wrap around.
// In "rnd" mode, values are randomly selected from the set.
// Thread-safe for concurrent access from multiple workers.
type StringSetGenerator struct {
	values  []string
	mode    Mode
	current atomic.Int64
}

// NewStringSetGenerator creates a new StringSetGenerator that generates values from
// the provided set. In "seq" mode, values cycle sequentially, wrapping back to
// the beginning after the last value. In "rnd" mode, values are randomly selected.
func NewStringSetGenerator(values []string, mode Mode) (*StringSetGenerator, error) {
	if len(values) == 0 {
		return nil, errors.New("values slice must not be empty")
	}
	if mode != SeqMode && mode != RndMode {
		return nil, errors.New("mode must be 'seq' or 'rnd'")
	}
	// Make a copy to prevent external modification
	valuesCopy := make([]string, len(values))
	copy(valuesCopy, values)
	return &StringSetGenerator{
		values: valuesCopy,
		mode:   mode,
	}, nil
}

// Next returns the next string from the set.
// In "seq" mode, values cycle through the set sequentially, then wrap back.
// In "rnd" mode, a random value from the set is returned.
func (g *StringSetGenerator) Next() any {
	if g.mode == RndMode {
		return g.values[mathrand.IntN(len(g.values))]
	}
	counter := g.current.Add(1) - 1 // get current value before increment
	index := counter % int64(len(g.values))
	return g.values[index]
}

// GetGeneratorType returns StringSetType.
func (g *StringSetGenerator) GetGeneratorType() GeneratorType {
	return StringSetType
}
