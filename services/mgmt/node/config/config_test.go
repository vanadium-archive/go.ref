package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"veyron/services/mgmt/node/config"

	"veyron2/services/mgmt/application"
)

// TestState checks that encoding/decoding State to child/from parent works
// as expected.
func TestState(t *testing.T) {
	currScript := filepath.Join(os.TempDir(), "fido/was/here")
	if err := os.MkdirAll(currScript, 0700); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	defer os.RemoveAll(currScript)
	currLink := filepath.Join(os.TempDir(), "familydog")
	if err := os.Symlink(currScript, currLink); err != nil {
		t.Fatalf("Symlink failed: %v", err)
	}
	defer os.Remove(currLink)
	state := &config.State{
		Name:        "fido",
		Previous:    "doesn't matter",
		Root:        "fidos/doghouse",
		Origin:      "pet/store",
		CurrentLink: currLink,
	}
	if err := state.Validate(); err != nil {
		t.Errorf("Config state %v failed to validate: %v", state, err)
	}
	encoded, err := state.Save(&application.Envelope{
		Title: "dog",
		Args:  []string{"roll-over", "play-dead"},
	})
	if err != nil {
		t.Errorf("Config state %v Save failed: %v", state, err)
	}
	for _, e := range encoded {
		pair := strings.SplitN(e, "=", 2)
		os.Setenv(pair[0], pair[1])
	}
	decodedState, err := config.Load()
	if err != nil {
		t.Errorf("Config state Load failed: %v", err)
	}
	expectedState := state
	expectedState.Envelope = &application.Envelope{
		Title: "dog",
		Args:  []string{"roll-over", "play-dead"},
	}
	expectedState.Name = ""
	expectedState.Previous = currScript
	if !reflect.DeepEqual(decodedState, expectedState) {
		t.Errorf("Decode state: want %v, got %v", expectedState, decodedState)
	}
}

// TestValidate checks the Validate method of State.
func TestValidate(t *testing.T) {
	state := &config.State{
		Name:        "schweinsteiger",
		Previous:    "a",
		Root:        "b",
		Origin:      "c",
		CurrentLink: "d",
	}
	if err := state.Validate(); err != nil {
		t.Errorf("Config state %v failed to validate: %v", state, err)
	}
	state.Root = ""
	if err := state.Validate(); err == nil {
		t.Errorf("Config state %v should have failed to validate.", state)
	}
	state.Root, state.CurrentLink = "a", ""
	if err := state.Validate(); err == nil {
		t.Errorf("Confi stateg %v should have failed to validate.", state)
	}
	state.CurrentLink, state.Name = "d", ""
	if err := state.Validate(); err == nil {
		t.Errorf("Config state %v should have failed to validate.", state)
	}
}

// TestQuoteEnv checks the QuoteEnv method.
func TestQuoteEnv(t *testing.T) {
	cases := []struct {
		before, after string
	}{
		{`a=b`, `a="b"`},
		{`a=`, `a=""`},
		{`a`, `a`},
		{`a=x y`, `a="x y"`},
		{`a="x y"`, `a="\"x y\""`},
		{`a='x y'`, `a="'x y'"`},
	}
	var input []string
	var want []string
	for _, c := range cases {
		input = append(input, c.before)
		want = append(want, c.after)
	}
	if got := config.QuoteEnv(input); !reflect.DeepEqual(want, got) {
		t.Errorf("QuoteEnv(%v) wanted %v, got %v instead", input, want, got)
	}
}
