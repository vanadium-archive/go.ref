// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build linux,!android

// Package gce functions to test whether the current process is running on
// Google Compute Engine, and to extract settings from this environment.
// Any server that knows it will only ever run on GCE can import this Profile,
// but in most cases, other Profiles will use the utility routines provided
// by it to test to ensure that they are running on GCE and to obtain
// metadata directly from its service APIs.
//
// TODO -- rename the package to "CloudVM" rather than gce, as it handles both gce
// and AWS, and perhaps, in future, other cases of NAT'ed VMs.
package gce

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sync"
	"time"

	"v.io/x/ref/lib/stats"
)

// This URL returns the external IP address assigned to the local GCE instance.
// If a HTTP GET request fails for any reason, this is not a GCE instance. If
// the result of the GET request doesn't contain a "Metadata-Flavor: Google"
// header, it is also not a GCE instance. The body of the document contains the
// external IP address, if present. Otherwise, the body is empty.
// See https://developers.google.com/compute/docs/metadata for details.
const gceUrl = "http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip"
const awsUrl = "http://169.254.169.254/latest/meta-data/public-ipv4"

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
		externalIP, isGCEErr = gceTest(gceUrl)
		if isGCEErr == nil {
			gceExportVariables()
		} else {
			// try AWS instead
			externalIP, isGCEErr = awsTest(awsUrl)
		}
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
	body, err := gceGetMeta(url, timeout)
	if err != nil {
		return nil, err
	}
	return net.ParseIP(body), nil
}

func gceExportVariables() {
	vars := []struct {
		name, url string
	}{
		{"system/gce/project-id", "http://metadata.google.internal/computeMetadata/v1/project/project-id"},
		{"system/gce/zone", "http://metadata.google.internal/computeMetadata/v1/instance/zone"},
	}
	for _, v := range vars {
		// At this point, we know we're on GCE. So, we might as well use a longer timeout.
		if body, err := gceGetMeta(v.url, 10*time.Second); err == nil {
			stats.NewString(v.name).Set(body)
		} else {
			stats.NewString(v.name).Set("unknown")
		}
	}
}

func gceGetMeta(url string, timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Metadata-Flavor", "Google")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("http error: %d", resp.StatusCode)
	}
	if flavor := resp.Header["Metadata-Flavor"]; len(flavor) != 1 || flavor[0] != "Google" {
		return "", fmt.Errorf("unexpected http header: %q", flavor)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func awsTest(url string) (net.IP, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http error: %d", resp.StatusCode)
	}
	if server := resp.Header["Server"]; len(server) != 1 || server[0] != "EC2ws" {
		return nil, fmt.Errorf("unexpected http Server header: %q", server)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return net.ParseIP(string(body)), nil
}
