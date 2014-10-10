package exec

import (
	"errors"
	"strings"
)

// Getenv retrieves the value of the given variable from the given
// slice of environment variable assignments.
func Getenv(env []string, name string) (string, error) {
	for _, v := range env {
		if strings.HasPrefix(v, name+"=") {
			return strings.TrimPrefix(v, name+"="), nil
		}
	}
	return "", errors.New("not found")
}

// Setenv updates / adds the value assignment for the given variable
// in the given slice of environment variable assigments.
func Setenv(env []string, name, value string) []string {
	newValue := name + "=" + value
	for i, v := range env {
		if strings.HasPrefix(v, name+"=") {
			env[i] = newValue
			return env
		}
	}
	return append(env, newValue)
}

// Mergeenv merges the values for the variables contained in 'other' with the
// values contained in 'base'.  If a variable exists in both, the value in
// 'other' takes precedence.
func Mergeenv(base, other []string) []string {
	otherValues := make(map[string]string)
	otherUsed := make(map[string]bool)
	for _, v := range other {
		if parts := strings.SplitN(v, "=", 2); len(parts) == 2 {
			otherValues[parts[0]] = parts[1]
		}
	}
	for i, v := range base {
		if parts := strings.SplitN(v, "=", 2); len(parts) == 2 {
			if otherValue, ok := otherValues[parts[0]]; ok {
				base[i] = parts[0] + "=" + otherValue
				otherUsed[parts[0]] = true
			}
		}
	}
	for k, v := range otherValues {
		if !otherUsed[k] {
			base = append(base, k+"="+v)
		}
	}
	return base
}
