package googleoauth

import (
	"encoding/json"
	"fmt"
	"io"
)

// ClientIDAndSecretFromJSON parses JSON-encoded API access information in 'r'
// and returns the extracted ClientID and ClientSecret.
// This JSON-encoded data is typically available as a download from the Google
// API Access console for your application
// (https://code.google.com/apis/console).
func ClientIDAndSecretFromJSON(r io.Reader) (id, secret string, err error) {
	var x map[string]interface{}
	if err = json.NewDecoder(r).Decode(&x); err != nil {
		return
	}
	var ok bool
	if x, ok = x["web"].(map[string]interface{}); !ok {
		err = fmt.Errorf("web not found")
		return
	}
	if id, ok = x["client_id"].(string); !ok {
		err = fmt.Errorf("web.client_id not found")
		return
	}
	if secret, ok = x["client_secret"].(string); !ok {
		err = fmt.Errorf("web.client_secret not found")
		return
	}
	return
}
