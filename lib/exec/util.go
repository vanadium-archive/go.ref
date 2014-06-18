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
