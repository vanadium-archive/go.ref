// +build linux

package gce

import (
	"fmt"
	"net"
	"net/http"
	"testing"
)

func startServer(t *testing.T) (net.Addr, func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	http.HandleFunc("/404", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	http.HandleFunc("/200_not_gce", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello")
	})
	http.HandleFunc("/gce_no_ip", func(w http.ResponseWriter, r *http.Request) {
		// When a GCE instance doesn't have an external IP address, the
		// request returns a 200 with an empty body.
		w.Header().Add("Metadata-Flavor", "Google")
		if m := r.Header["Metadata-Flavor"]; len(m) != 1 || m[0] != "Google" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	})
	http.HandleFunc("/gce_with_ip", func(w http.ResponseWriter, r *http.Request) {
		// When a GCE instance has an external IP address, the request
		// returns the IP address as body.
		w.Header().Add("Metadata-Flavor", "Google")
		if m := r.Header["Metadata-Flavor"]; len(m) != 1 || m[0] != "Google" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprintf(w, "1.2.3.4")
	})

	go http.Serve(l, nil)
	return l.Addr(), func() { l.Close() }
}

func TestGCE(t *testing.T) {
	addr, stop := startServer(t)
	defer stop()
	baseURL := "http://" + addr.String()

	if ip, err := gceTest(baseURL + "/404"); err == nil || ip != nil {
		t.Errorf("expected error, but not got nil")
	}
	if ip, err := gceTest(baseURL + "/200_not_gce"); err == nil || ip != nil {
		t.Errorf("expected error, but not got nil")
	}
	if ip, err := gceTest(baseURL + "/gce_no_ip"); err != nil || ip != nil {
		t.Errorf("Unexpected result. Got (%v, %v), want nil:nil", ip, err)
	}
	if ip, err := gceTest(baseURL + "/gce_with_ip"); err != nil || ip.String() != "1.2.3.4" {
		t.Errorf("Unexpected result. Got (%v, %v), want nil:1.2.3.4", ip, err)
	}
}
