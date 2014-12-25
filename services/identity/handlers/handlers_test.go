package handlers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"v.io/veyron/veyron2/security"

	tsecurity "v.io/veyron/veyron/lib/testutil/security"
)

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
