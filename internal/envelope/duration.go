package envelope

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a YAML-friendly time.Duration ("200ms", "2s", or integer nanoseconds).
type Duration time.Duration

// Std returns the standard library duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// MarshalYAML encodes as a human string.
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

// UnmarshalYAML accepts strings like "200ms" or integer nanoseconds.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Tag == "!!int" || value.Tag == "!!float" {
			var n int64
			if err := value.Decode(&n); err != nil {
				return err
			}
			*d = Duration(n)
			return nil
		}
		var s string
		if err := value.Decode(&s); err != nil {
			return err
		}
		if s == "" || s == "0" {
			*d = 0
			return nil
		}
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("duration: %w", err)
		}
		*d = Duration(parsed)
		return nil
	default:
		return fmt.Errorf("duration: want scalar, got kind %v", value.Kind)
	}
}
