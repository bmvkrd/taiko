package engine

import (
	"fmt"
	"regexp"

	"github.com/bmvkrd/taiko/internal/config"
	"github.com/bmvkrd/taiko/internal/generator"
)

type Substitutor struct {
	generators map[string]generator.Generator
	varPattern *regexp.Regexp
}

func NewSubstitutor(variables []config.Variable) (*Substitutor, error) {
	generators := make(map[string]generator.Generator, len(variables))
	for _, v := range variables {
		gen, err := fromVariable(v)
		if err != nil {
			return nil, fmt.Errorf("failed to create generator for variable '%s': %w", v.Name, err)
		}
		generators[v.Name] = gen
	}
	return &Substitutor{
		generators: generators,
		varPattern: regexp.MustCompile(`\{\{(\w+)\}\}`),
	}, nil
}

// NewValues generates a fresh value for each configured variable. Call this
// once per request, then pass the result to Apply for each template string.
// Returns nil when no variables are configured.
func (s *Substitutor) NewValues() map[string]string {
	if len(s.generators) == 0 {
		return nil
	}
	values := make(map[string]string, len(s.generators))
	for name, gen := range s.generators {
		values[name] = fmt.Sprintf("%v", gen.Next())
	}
	return values
}

// Apply replaces {{variable}} placeholders in template with values from the map.
// values should come from a single NewValues call so that all templates within
// the same request share the same generated values.
func (s *Substitutor) Apply(template string, values map[string]string) string {
	if len(values) == 0 {
		return template
	}
	return s.varPattern.ReplaceAllStringFunc(template, func(match string) string {
		varName := match[2 : len(match)-2]
		if val, ok := values[varName]; ok {
			return val
		}
		return match
	})
}

func fromVariable(v config.Variable) (generator.Generator, error) {
	switch v.Type {
	case string(generator.IntRangeType):
		mode := generator.Mode(v.Generator["mode"].(string))
		min := toInt(v.Generator["min"])
		max := toInt(v.Generator["max"])
		return generator.NewIntRangeGenerator(min, max, mode)
	case string(generator.IntSetType):
		mode := generator.Mode(v.Generator["mode"].(string))
		values := toIntSlice(v.Generator["values"])
		return generator.NewIntSetGenerator(values, mode)
	case string(generator.UUIDType):
		return generator.NewUUIDGenerator(), nil
	case string(generator.StringSetType):
		mode := generator.Mode(v.Generator["mode"].(string))
		values := toStringSlice(v.Generator["values"])
		return generator.NewStringSetGenerator(values, mode)
	case string(generator.TimestampType):
		format := generator.TimestampFormat(v.Generator["format"].(string))
		return generator.NewTimestampGenerator(format)
	default:
		return nil, fmt.Errorf("unknown generator type: %s", v.Type)
	}
}

func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	default:
		return 0
	}
}

func toIntSlice(v any) []int {
	switch val := v.(type) {
	case []int:
		return val
	case []any:
		result := make([]int, len(val))
		for i, item := range val {
			result[i] = toInt(item)
		}
		return result
	default:
		return nil
	}
}

func toStringSlice(v any) []string {
	switch val := v.(type) {
	case []string:
		return val
	case []any:
		result := make([]string, len(val))
		for i, item := range val {
			if s, ok := item.(string); ok {
				result[i] = s
			}
		}
		return result
	default:
		return nil
	}
}
