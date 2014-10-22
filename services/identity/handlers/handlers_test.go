package handlers

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/security"

	vsecurity "veyron.io/veyron/veyron/security"
)

func TestPublicKey(t *testing.T) {
	p, err := vsecurity.NewPrincipal()
	if err != nil {
		t.Fatal(err)
	}
	key := p.PublicKey()
	ts := httptest.NewServer(PublicKey{key})
	defer ts.Close()
	response, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := security.UnmarshalPublicKey(bytes)
	if err != nil {
		t.Fatal(err)
	}
	if want := key; !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}
}
