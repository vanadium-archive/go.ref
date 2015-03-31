// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package envvar

import (
	"os"
	"reflect"
	"testing"
)

// Set an environment variable and return a function to undo it.
// Typical usage:
//   defer setenv(t, "VAR", "VALUE")()
func setenv(t *testing.T, name, value string) func() {
	oldval := os.Getenv(name)
	if err := os.Setenv(name, value); err != nil {
		t.Fatalf("Failed to set %q to %q: %v", name, value, err)
		return func() {}
	}
	return func() {
		if err := os.Setenv(name, oldval); err != nil {
			t.Fatalf("Failed to restore %q to %q: %v", name, oldval, err)
		}
	}
}

func TestNamespaceRoots(t *testing.T) {
	defer setenv(t, NamespacePrefix, "NS1")()
	defer setenv(t, NamespacePrefix+"_BLAH", "NS_BLAH")()

	wantm := map[string]string{
		"V23_NAMESPACE":      "NS1",
		"V23_NAMESPACE_BLAH": "NS_BLAH",
	}
	wantl := []string{"NS1", "NS_BLAH"}

	gotm, gotl := NamespaceRoots()
	if !reflect.DeepEqual(wantm, gotm) {
		t.Errorf("Got %v want %v", gotm, wantm)
	}
	if !reflect.DeepEqual(wantl, gotl) {
		t.Errorf("Got %v want %v", gotl, wantl)
	}
}

func TestClearCredentials(t *testing.T) {
	defer setenv(t, Credentials, "FOO")()
	if got, want := os.Getenv(Credentials), "FOO"; got != want {
		t.Errorf("Got %q, want %q", got, want)
	}
	ClearCredentials()
	if got := os.Getenv(Credentials); got != "" {
		t.Errorf("Got %q, wanted empty string", got)
	}
}
