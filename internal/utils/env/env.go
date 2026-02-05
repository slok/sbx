package env

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var envKeyRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func ParseSpecs(specs []string) (map[string]string, error) {
	env := make(map[string]string, len(specs))

	for _, spec := range specs {
		if spec == "" {
			return nil, fmt.Errorf("environment variable spec cannot be empty")
		}

		if key, value, ok := strings.Cut(spec, "="); ok {
			if !isValidKey(key) {
				return nil, fmt.Errorf("invalid environment variable key %q", key)
			}

			env[key] = value
			continue
		}

		if !isValidKey(spec) {
			return nil, fmt.Errorf("invalid environment variable key %q", spec)
		}

		value, ok := os.LookupEnv(spec)
		if !ok {
			return nil, fmt.Errorf("environment variable %q is not set", spec)
		}

		env[spec] = value
	}

	return env, nil
}

func MergeMaps(base map[string]string, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return map[string]string{}
	}

	merged := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}

	return merged
}

func isValidKey(k string) bool {
	return envKeyRegexp.MatchString(k)
}
