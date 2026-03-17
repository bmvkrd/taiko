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
		modeStr, err := stringFromMap(v.Generator, "mode")
		if err != nil {
			return nil, err
		}
		min, err := intFromMap(v.Generator, "min")
		if err != nil {
			return nil, err
		}
		max, err := intFromMap(v.Generator, "max")
		if err != nil {
			return nil, err
		}
		return generator.NewIntRangeGenerator(min, max, generator.Mode(modeStr))
	case string(generator.IntSetType):
		modeStr, err := stringFromMap(v.Generator, "mode")
		if err != nil {
			return nil, err
		}
		values, err := intSliceFromMap(v.Generator, "values")
		if err != nil {
			return nil, err
		}
		return generator.NewIntSetGenerator(values, generator.Mode(modeStr))
	case string(generator.UUIDType):
		return generator.NewUUIDGenerator(), nil
	case string(generator.StringSetType):
		modeStr, err := stringFromMap(v.Generator, "mode")
		if err != nil {
			return nil, err
		}
		values, err := stringSliceFromMap(v.Generator, "values")
		if err != nil {
			return nil, err
		}
		return generator.NewStringSetGenerator(values, generator.Mode(modeStr))
	case string(generator.TimestampType):
		formatStr, err := stringFromMap(v.Generator, "format")
		if err != nil {
			return nil, err
		}
		return generator.NewTimestampGenerator(generator.TimestampFormat(formatStr))
	default:
		return nil, fmt.Errorf("unknown generator type: %s", v.Type)
	}
}

func stringFromMap(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("missing required field %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("field %q must be a string, got %T", key, v)
	}
	return s, nil
}

func intFromMap(m map[string]any, key string) (int, error) {
	v, ok := m[key]
	if !ok {
		return 0, fmt.Errorf("missing required field %q", key)
	}
	switch val := v.(type) {
	case int:
		return val, nil
	case int64:
		return int(val), nil
	case float64:
		return int(val), nil
	default:
		return 0, fmt.Errorf("field %q must be a number, got %T", key, v)
	}
}

func intSliceFromMap(m map[string]any, key string) ([]int, error) {
	v, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("missing required field %q", key)
	}
	switch val := v.(type) {
	case []int:
		return val, nil
	case []any:
		result := make([]int, len(val))
		for i, item := range val {
			switch n := item.(type) {
			case int:
				result[i] = n
			case int64:
				result[i] = int(n)
			case float64:
				result[i] = int(n)
			default:
				return nil, fmt.Errorf("field %q[%d] must be a number, got %T", key, i, item)
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("field %q must be an array, got %T", key, v)
	}
}

func stringSliceFromMap(m map[string]any, key string) ([]string, error) {
	v, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("missing required field %q", key)
	}
	switch val := v.(type) {
	case []string:
		return val, nil
	case []any:
		result := make([]string, len(val))
		for i, item := range val {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("field %q[%d] must be a string, got %T", key, i, item)
			}
			result[i] = s
		}
		return result, nil
	default:
		return nil, fmt.Errorf("field %q must be an array, got %T", key, v)
	}
}
