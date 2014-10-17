package handlers

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"

	_ "veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/services/identity/util"
)

func TestPublicKey(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Cleanup()
	ts := httptest.NewServer(PublicKey{r.Identity().PublicID()})
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
	if want := r.Identity().PublicKey(); !reflect.DeepEqual(got, want) {
		t.Errorf("Got %v, want %v", got, want)
	}
}

func TestRandom(t *testing.T) {
	r, err := rt.New()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Cleanup()
	ts := httptest.NewServer(Random{r})
	defer ts.Close()

	got, err := parseResponse(http.Get(ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := got.(security.PrivateID); !ok {
		t.Fatalf("Got %T want security.PrivateID", got, id)
	}
}

func parseResponse(r *http.Response, err error) (interface{}, error) {
	if err != nil {
		return nil, err
	}
	b64, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	var parsed interface{}
	if err := util.Base64VomDecode(string(b64), &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}
