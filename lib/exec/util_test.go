// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package exec

import (
	"testing"
)

func TestEnv(t *testing.T) {
	env := make([]string, 0)
	env = Setenv(env, "NAME", "VALUE1")
	if expected, got := 1, len(env); expected != got {
		t.Fatalf("Unexpected length of environment variable slice: expected %d, got %d", expected, got)
	}
	if expected, got := "NAME=VALUE1", env[0]; expected != got {
		t.Fatalf("Unexpected element in the environment variable slice: expected %q, got %q", expected, got)
	}
	env = Setenv(env, "NAME", "VALUE2")
	if expected, got := 1, len(env); expected != got {
		t.Fatalf("Unexpected length of environment variable slice: expected %d, got %d", expected, got)
	}
	if expected, got := "NAME=VALUE2", env[0]; expected != got {
		t.Fatalf("Unexpected element in the environment variable slice: expected %q, got %q", expected, got)
	}
	value, err := Getenv(env, "NAME")
	if err != nil {
		t.Fatalf("Unexpected error when looking up environment variable value: %v", err)
	}
	if expected, got := "VALUE2", value; expected != got {
		t.Fatalf("Unexpected value of an environment variable: expected %q, got %q", expected, got)
	}
	value, err = Getenv(env, "NONAME")
	if err == nil {
		t.Fatalf("Expected error when looking up environment variable value, got none (value: %q)", value)
	}
}

func TestMerge(t *testing.T) {
	env := []string{"ANIMAL=GOPHER", "METAL=CHROMIUM"}
	env = Mergeenv(env, []string{"CAR=VEYRON", "METAL=VANADIUM"})
	if want, got := 3, len(env); want != got {
		t.Fatalf("Expected %d env vars, got %d instead", want, got)
	}
	for n, want := range map[string]string{
		"ANIMAL": "GOPHER",
		"CAR":    "VEYRON",
		"METAL":  "VANADIUM",
	} {
		if got, err := Getenv(env, n); err != nil {
			t.Fatalf("Expected a value when looking up %q, got none", n)
		} else if got != want {
			t.Fatalf("Unexpected value of environment variable %q: expected %q, got %q", n, want, got)
		}
	}
}
