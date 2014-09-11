package googleoauth

import (
	"strings"
	"testing"
	"time"
)

func TestClientIDAndSecretFromJSON(t *testing.T) {
	json := `{"web":{"auth_uri":"https://accounts.google.com/o/oauth2/auth","client_secret":"SECRET","token_uri":"https://accounts.google.com/o/oauth2/token","client_email":"EMAIL","redirect_uris":["http://redirecturl"],"client_id":"ID","auth_provider_x509_cert_url":"https://www.googleapis.com/oauth2/v1/certs","javascript_origins":["http://javascriptorigins"]}}`
	id, secret, err := ClientIDAndSecretFromJSON(strings.NewReader(json))
	if err != nil {
		t.Error(err)
	}
	if id != "ID" {
		t.Errorf("Got %q want %q", id, "ID")
	}
	if secret != "SECRET" {
		t.Errorf("Got %q want %q", secret, "SECRET")
	}
}

func TestTokenRevocationCaveatMap(t *testing.T) {
	d := time.Millisecond * 10
	tokenMap := newTokenRevocationCaveatMap(d)
	// Test non-existent token and non-existent caveatID.
	if tokenMap.Exists("NEToken", "NECaveatID") {
		t.Errorf("found non-existent token with non-existent caveatID")
	}
	tokenMap.Insert("Token", "CaveatID")
	// Test token and caveatID when both exist.
	if !tokenMap.Exists("Token", "CaveatID") {
		t.Errorf("did not find existing token and caveatID")
	}
	// Test that after timeout they don't exist.
	time.Sleep(3 * d)
	if tokenMap.Exists("Token", "CaveatID") {
		t.Errorf("token and caveatID should have been removed")
	}

	tokenMap.Insert("Token", "CaveatID")
	// Test non-existent token and existent caveatID.
	if tokenMap.Exists("NEToken", "CaveatID") {
		t.Errorf("found non-existent token with existing caveatID")
	}
	// Test existent token and non-existent caveatID.
	if tokenMap.Exists("Token", "NECaveatID") {
		t.Errorf("found existing token with non-existent caveatID")
	}

}
