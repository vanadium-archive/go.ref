package googleoauth

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// ClientIDFromJSON parses JSON-encoded API access information in 'r' and returns
// the extracted ClientID.
// This JSON-encoded data is typically available as a download from the Google
// API Access console for your application
// (https://code.google.com/apis/console).
func ClientIDFromJSON(r io.Reader) (id string, err error) {
	var data map[string]interface{}
	var typ string
	if data, typ, err = decodeAccessMapFromJSON(r); err != nil {
		return
	}
	var ok bool
	if id, ok = data["client_id"].(string); !ok {
		err = fmt.Errorf("%s.client_id not found", typ)
		return
	}
	return
}

// ClientIDAndSecretFromJSON parses JSON-encoded API access information in 'r'
// and returns the extracted ClientID and ClientSecret.
// This JSON-encoded data is typically available as a download from the Google
// API Access console for your application
// (https://code.google.com/apis/console).
func ClientIDAndSecretFromJSON(r io.Reader) (id, secret string, err error) {
	var data map[string]interface{}
	var typ string
	if data, typ, err = decodeAccessMapFromJSON(r); err != nil {
		return
	}
	var ok bool
	if id, ok = data["client_id"].(string); !ok {
		err = fmt.Errorf("%s.client_id not found", typ)
		return
	}
	if secret, ok = data["client_secret"].(string); !ok {
		err = fmt.Errorf("%s.client_secret not found", typ)
		return
	}
	return
}

func decodeAccessMapFromJSON(r io.Reader) (data map[string]interface{}, typ string, err error) {
	var full map[string]interface{}
	if err = json.NewDecoder(r).Decode(&full); err != nil {
		return
	}
	var ok bool
	typ = "web"
	if data, ok = full[typ].(map[string]interface{}); !ok {
		typ = "installed"
		if data, ok = full[typ].(map[string]interface{}); !ok {
			err = fmt.Errorf("web or installed configuration not found")
		}
	}
	return
}

// Map that maintains storage of tokens and caveatIDs and removal after specified timeout.
// This is need to ensure the correct identities have revoke access from the identity server.
type tokenRevocationCaveatMap struct {
	mapIndex  int // The index of the current map being. This will be the index of the map that should be inserted into.
	tokenMaps [2]map[tokenCaveatIDKey]bool
	mu        sync.RWMutex // guards mapIndex and tokenMaps
}

type tokenCaveatIDKey struct {
	token, caveatID string
}

// newTokenRevocationCaveatMap returns a map from tokens to CaveatIDs such that every Insert-ed entry will Exist at least
// until 'timeout' and at most until 2*'timeout'
func newTokenRevocationCaveatMap(timeout time.Duration) *tokenRevocationCaveatMap {
	var tokenMaps [2]map[tokenCaveatIDKey]bool
	for i := 0; i < 2; i++ {
		tokenMaps[i] = make(map[tokenCaveatIDKey]bool)
	}
	m := tokenRevocationCaveatMap{mapIndex: 0, tokenMaps: tokenMaps}
	go m.timeoutLoop(timeout)
	return &m
}

func (m *tokenRevocationCaveatMap) Insert(token, caveatID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokenMaps[m.mapIndex][tokenCaveatIDKey{token, caveatID}] = true
}

func (m *tokenRevocationCaveatMap) Exists(token, caveatID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := 0; i < 2; i++ {
		if _, exists := m.tokenMaps[i][tokenCaveatIDKey{token, caveatID}]; exists {
			return true
		}
	}
	return false
}

func (m *tokenRevocationCaveatMap) timeoutLoop(timeout time.Duration) {
	for {
		time.Sleep(timeout)
		m.mu.Lock()
		m.mapIndex = (m.mapIndex + 1) % 2
		m.tokenMaps[m.mapIndex] = make(map[tokenCaveatIDKey]bool)
		m.mu.Unlock()
	}
}
