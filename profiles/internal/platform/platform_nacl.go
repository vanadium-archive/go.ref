// +build nacl

package platform

import (
	"v.io/v23"
)

// Platform returns the description of the Platform this process is running on.
// A default value for v23.Platform is provided even if an error is
// returned; nil is never returned for the first return result.
func Platform() (*v23.Platform, error) {
	d := &v23.Platform{
		Vendor:  "google",
		Model:   "generic",
		System:  "nacl",
		Version: "0",
		Release: "0",
		Machine: "0",
		Node:    "0",
	}
	return d, nil
}
