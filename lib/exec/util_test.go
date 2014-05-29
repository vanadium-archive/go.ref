package exec

import (
	"testing"
)

func TestEnv(t *testing.T) {
	env := make([]string, 0)
	env = SetEnv(env, "NAME", "VALUE1")
	if expected, got := 1, len(env); expected != got {
		t.Fatalf("Unexpected length of environment variable slice: expected %d, got %d", expected, got)
	}
	if expected, got := "NAME=VALUE1", env[0]; expected != got {
		t.Fatalf("Unexpected element in the environment variable slice: expected %d, got %d", expected, got)
	}
	env = SetEnv(env, "NAME", "VALUE2")
	if expected, got := 1, len(env); expected != got {
		t.Fatalf("Unexpected length of environment variable slice: expected %d, got %d", expected, got)
	}
	if expected, got := "NAME=VALUE2", env[0]; expected != got {
		t.Fatalf("Unexpected element in the environment variable slice: expected %d, got %d", expected, got)
	}
	value, err := GetEnv(env, "NAME")
	if err != nil {
		t.Fatalf("Unexpected error when looking up environment variable value: %v", err)
	}
	if expected, got := "VALUE2", value; expected != got {
		t.Fatalf("Unexpected value of an environment variable: expected %d, got %d", expected, got)
	}
	value, err = GetEnv(env, "NONAME")
	if err == nil {
		t.Fatalf("Expected error when looking up environment variable value, got none", value)
	}
}
