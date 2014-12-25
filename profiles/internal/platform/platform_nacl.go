// +build nacl

package platform

import (
	"v.io/veyron/veyron2"
)

// Platform returns the description of the Platform this process is running on.
// A default value for veyron2.Platform is provided even if an error is
// returned; nil is never returned for the first return result.
func Platform() (*veyron2.Platform, error) {
	d := &veyron2.Platform{
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
