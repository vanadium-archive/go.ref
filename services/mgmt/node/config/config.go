// Package config handles configuration state passed across instances of the
// node manager.
//
// The State object captures setting that the node manager needs to be aware of
// when it starts.  This is passed to the first invocation of the node manager,
// and then passed down from old node manager to new node manager upon update.
// The node manager has an implementation-dependent mechanism for parsing and
// passing state, which is encapsulated by the state sub-package (currently, the
// mechanism uses environment variables).  When instantiating a new instance of
// the node manager service, the developer needs to pass in a copy of State.
// They can obtain this by calling Load, which captures any config state passed
// by a previous version of node manager during update.  Any new version of the
// node manager must be able to decode a previous version's config state, even
// if the new version changes the mechanism for passing this state (that is,
// node manager implementations must be backward-compatible as far as accepting
// and passing config state goes).  TODO(caprita): add config state versioning?
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"veyron.io/veyron/veyron/lib/flags/consts"
	"veyron.io/veyron/veyron2/services/mgmt/application"
)

// State specifies how the node manager is configured.  This should encapsulate
// what the node manager needs to know and/or be able to mutate about its
// environment.
type State struct {
	// Name is the node manager's object name.  Must be non-empty.
	Name string
	// Envelope is the node manager's application envelope.  If nil, any
	// envelope fetched from the application repository will trigger an
	// update.
	Envelope *application.Envelope
	// Previous holds the local path to the previous version of the node
	// manager.  If empty, revert is disabled.
	Previous string
	// Root is the directory on the local filesystem that contains
	// the applications' workspaces.  Must be non-empty.
	Root string
	// Origin is the application repository object name for the node
	// manager application.  If empty, update is disabled.
	Origin string
	// CurrentLink is the local filesystem soft link that should point to
	// the version of the node manager binary/script through which node
	// manager is started.  Node manager is expected to mutate this during
	// a self-update.  Must be non-empty.
	CurrentLink string
	// Helper is the path to the setuid helper for running applications as
	// specific users.
	Helper string
}

// Validate checks the config state.
func (c *State) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("Name cannot be empty")
	}
	if c.Root == "" {
		return fmt.Errorf("Root cannot be empty")
	}
	if c.CurrentLink == "" {
		return fmt.Errorf("CurrentLink cannot be empty")
	}
	if c.Helper == "" {
		return fmt.Errorf("Helper must be specified")
	}
	return nil
}

// Load reconstructs the config state passed to the node manager (presumably by
// the parent node manager during an update).  Currently, this is done via
// environment variables.
func Load() (*State, error) {
	var env *application.Envelope
	if jsonEnvelope := os.Getenv(EnvelopeEnv); jsonEnvelope != "" {
		env = new(application.Envelope)
		if err := json.Unmarshal([]byte(jsonEnvelope), env); err != nil {
			return nil, fmt.Errorf("failed to decode envelope from %v: %v", jsonEnvelope, err)
		}
	}
	return &State{
		Envelope:    env,
		Previous:    os.Getenv(PreviousEnv),
		Root:        os.Getenv(RootEnv),
		Origin:      os.Getenv(OriginEnv),
		CurrentLink: os.Getenv(CurrentLinkEnv),
		Helper:      os.Getenv(HelperEnv),
	}, nil
}

// Save serializes the config state meant to be passed to a child node manager
// during an update, returning a slice of "key=value" strings, which are
// expected to be stuffed into environment variable settings by the caller.
func (c *State) Save(envelope *application.Envelope) ([]string, error) {
	jsonEnvelope, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("failed to encode envelope %v: %v", envelope, err)
	}
	currScript, err := filepath.EvalSymlinks(c.CurrentLink)
	if err != nil {
		return nil, fmt.Errorf("EvalSymlink failed: %v", err)
	}
	settings := map[string]string{
		EnvelopeEnv:    string(jsonEnvelope),
		PreviousEnv:    currScript,
		RootEnv:        c.Root,
		OriginEnv:      c.Origin,
		CurrentLinkEnv: c.CurrentLink,
		HelperEnv:      c.Helper,
	}
	// We need to manually pass the namespace roots to the child, since we
	// currently don't have a way for the child to obtain this information
	// from a config service at start-up.
	for _, ev := range os.Environ() {
		p := strings.SplitN(ev, "=", 2)
		if len(p) != 2 {
			continue
		}
		k, v := p[0], p[1]
		if strings.HasPrefix(k, consts.NamespaceRootPrefix) {
			settings[k] = v
		}
	}
	var ret []string
	for k, v := range settings {
		ret = append(ret, k+"="+v)
	}
	return ret, nil
}

// QuoteEnv wraps environment variable values in double quotes, making them
// suitable for inclusion in a bash script.
func QuoteEnv(env []string) (ret []string) {
	for _, e := range env {
		if eqIdx := strings.Index(e, "="); eqIdx > 0 {
			ret = append(ret, fmt.Sprintf("%s=%q", e[:eqIdx], e[eqIdx+1:]))
		} else {
			ret = append(ret, e)
		}
	}
	return
}
