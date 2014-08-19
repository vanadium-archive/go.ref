// +build linux

// Package gce functions to test whether the current process is running on
// Google Compute Engine, and to extract settings from this environment.
// Any server that knows it will only ever run on GCE can import this Profile,
// but in most cases, other Profiles will use the utility routines provided
// by it to test to ensure that they are running on GCE and to obtain
// metadata directly from its service APIs.
package gce

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sync"
	"time"
)

// This URL returns the external IP address assigned to the local GCE instance.
// If a HTTP GET request fails for any reason, this is not a GCE instance. If
// the result of the GET request doesn't contain a "Metadata-Flavor: Google"
// header, it is also not a GCE instance. The body of the document contains the
// external IP address, if present. Otherwise, the body is empty.
// See https://developers.google.com/compute/docs/metadata for details.
const url = "http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip"

// How long to wait for the HTTP request to return.
const timeout = time.Second

var (
	once       sync.Once
	isGCEErr   error
	externalIP net.IP
)

// RunningOnGCE returns true if the current process is running
// on a Google Compute Engine instance.
func RunningOnGCE() bool {
	once.Do(func() {
		externalIP, isGCEErr = gceTest(url)
	})
	return isGCEErr == nil
}

// ExternalIPAddress returns the external IP address of this
// Google Compute Engine instance, or nil if there is none. Must be
// called after RunningOnGCE.
func ExternalIPAddress() (net.IP, error) {
	return externalIP, isGCEErr
}

func gceTest(url string) (net.IP, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Metadata-Flavor", "Google")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http error: %d", resp.StatusCode)
	}
	if flavor := resp.Header["Metadata-Flavor"]; len(flavor) != 1 || flavor[0] != "Google" {
		return nil, fmt.Errorf("unexpected http header: %q", flavor)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return net.ParseIP(string(body)), nil
}
