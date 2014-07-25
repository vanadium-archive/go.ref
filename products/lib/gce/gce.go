// Package gce provides a way to test whether the current process is running on
// Google Compute Engine, and to extract settings from this environment.
package gce

import (
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
const url = "http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip"

// How long to wait for the HTTP request to return.
const timeout = time.Second

var (
	once       sync.Once
	isGCE      bool
	externalIP net.IP
)

// RunningOnGoogleComputeEngine returns true if the current process is running
// on a Google Compute Engine instance.
func RunningOnGoogleComputeEngine() bool {
	once.Do(func() {
		isGCE, externalIP = googleComputeEngineTest(url)
	})
	return isGCE
}

// GoogleComputeEngineExternalIPAddress returns the external IP address of this
// Google Compute Engine instance, or nil if there is none. Must be called after
// RunningOnGoogleComputeEngine.
func GoogleComputeEngineExternalIPAddress() net.IP {
	return externalIP
}

func googleComputeEngineTest(url string) (bool, net.IP) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, nil
	}
	req.Header.Add("Metadata-Flavor", "Google")
	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false, nil
	}
	if flavor := resp.Header["Metadata-Flavor"]; len(flavor) != 1 || flavor[0] != "Google" {
		return false, nil
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return true, nil
	}
	return true, net.ParseIP(string(body))
}
