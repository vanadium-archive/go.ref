package handlers

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"veyron.io/veyron/veyron2/security"

	tsecurity "veyron.io/veyron/veyron/lib/testutil/security"
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

func TestBlessingRoot(t *testing.T) {
	blessingNames := []string{"test-blessing-name-1", "test-blessing-name-2"}
	p := tsecurity.NewPrincipal(blessingNames...)

	ts := httptest.NewServer(BlessingRoot{p})
	defer ts.Close()
	response, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(response.Body)
	var res BlessingRootResponse
	if err := dec.Decode(&res); err != nil {
		t.Fatal(err)
	}

	// Check that the names are correct.
	sort.Strings(blessingNames)
	sort.Strings(res.Names)
	if !reflect.DeepEqual(res.Names, blessingNames) {
		t.Errorf("Response has incorrect name. Got %v, want %v", res.Names, blessingNames)
	}

	// Check that the public key is correct.
	gotMarshalled, err := base64.URLEncoding.DecodeString(res.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err := security.UnmarshalPublicKey(gotMarshalled)
	if err != nil {
		t.Fatal(err)
	}
	if want := p.PublicKey(); !reflect.DeepEqual(got, want) {
		t.Errorf("Response has incorrect public key.  Got %v, want %v", got, want)
	}
}
