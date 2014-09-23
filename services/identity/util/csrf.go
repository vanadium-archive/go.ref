package util

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"veyron.io/veyron/veyron2/vlog"
	"veyron.io/veyron/veyron2/vom"
)

const (
	cookieLen = 16
	keyLength = 16
)

type CSRFCop struct {
	key []byte
}

func NewCSRFCop() (*CSRFCop, error) {
	key := make([]byte, keyLength)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("newCSRFCop failed: %v", err)
	}
	return &CSRFCop{key}, nil
}

// NewToken creates an anti-cross-site-request-forgery, aka CSRF aka XSRF token
// with some data bound to it that can be obtained by ValidateToken.
// It returns an error if the token could not be created.
func (c *CSRFCop) NewToken(w http.ResponseWriter, r *http.Request, cookieName string, data interface{}) (string, error) {
	cookieValue, err := c.MaybeSetCookie(w, r, cookieName)
	if err != nil {
		return "", fmt.Errorf("bad cookie: %v", err)
	}
	buf := &bytes.Buffer{}
	if data != nil {
		if err := vom.NewEncoder(buf).Encode(data); err != nil {
			return "", err
		}
	}
	m, err := c.createMacaroon(buf.Bytes(), cookieValue)
	if err != nil {
		return "", err
	}
	return b64encode(append(m.Data, m.HMAC...)), nil
}

// ValidateToken checks the validity of the provided CSRF token for the
// provided request, and extracts the data encoded in the token into 'decoded'.
// If the token is invalid, return an error. This error should not be shown to end users,
// it is meant for the consumption by the server process only.
func (c *CSRFCop) ValidateToken(token string, req *http.Request, cookieName string, decoded interface{}) error {
	cookie, err := req.Cookie(cookieName)
	if err != nil {
		return err
	}
	cookieValue, err := decodeCookieValue(cookie.Value)
	if err != nil {
		return fmt.Errorf("invalid cookie")
	}
	decToken, err := b64decode(token)
	if err != nil {
		return fmt.Errorf("invalid token: %v", err)
	}
	m := macaroon{
		Data: decToken[:len(decToken)-sha256.Size],
		HMAC: decToken[len(decToken)-sha256.Size:],
	}
	if decoded != nil {
		if err := vom.NewDecoder(bytes.NewBuffer(m.Data)).Decode(decoded); err != nil {
			return fmt.Errorf("invalid token data: %v", err)
		}
	}
	return c.verifyMacaroon(m, cookieValue)
}

// macaroon encapsulates an arbitrary slice of data with an HMAC for integrity protection.
// Term borrowed from http://research.google.com/pubs/pub41892.html.
type macaroon struct {
	Data, HMAC []byte
}

func (c *CSRFCop) createMacaroon(input, hiddenInput []byte) (*macaroon, error) {
	m := &macaroon{Data: input}
	var err error
	if m.HMAC, err = c.hmac(m.Data, hiddenInput); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *CSRFCop) verifyMacaroon(m macaroon, hiddenInput []byte) error {
	hm, err := c.hmac(m.Data, hiddenInput)
	if err != nil {
		return fmt.Errorf("invalid macaroon: %v", err)
	}
	if !hmac.Equal(m.HMAC, hm) {
		return fmt.Errorf("invalid macaroon, HMAC does not match")
	}
	return nil
}

func (c *CSRFCop) hmac(input, hiddenInput []byte) ([]byte, error) {
	hm := hmac.New(sha256.New, c.key)
	var err error
	// We hash inputs and hiddenInputs to make each a fixed length to avoid
	// ambiguity with simple concatenation of bytes.
	w := func(data []byte) error {
		tmp := sha256.Sum256(data)
		_, err = hm.Write(tmp[:])
		return err
	}
	if err := w(input); err != nil {
		return nil, err
	}
	if err := w(hiddenInput); err != nil {
		return nil, err
	}
	return hm.Sum(nil), nil
}

func (*CSRFCop) MaybeSetCookie(w http.ResponseWriter, req *http.Request, cookieName string) ([]byte, error) {
	cookie, err := req.Cookie(cookieName)
	switch err {
	case nil:
		if v, err := decodeCookieValue(cookie.Value); err == nil {
			return v, nil
		}
		vlog.Infof("Invalid cookie: %#v, err: %v. Regenerating one.", cookie, err)
	case http.ErrNoCookie:
		// Intentionally blank: Cookie will be generated below.
	default:
		vlog.Infof("Error decoding cookie %q in request: %v. Regenerating one.", cookieName, err)
	}
	cookie, v := newCookie(cookieName)
	if cookie == nil || v == nil {
		return nil, fmt.Errorf("failed to create cookie")
	}
	http.SetCookie(w, cookie)
	// We need to add the cookie to the request also to prevent repeatedly resetting cookies on multiple
	// calls from the same request.
	req.AddCookie(cookie)
	return v, nil
}

func newCookie(cookieName string) (*http.Cookie, []byte) {
	b := make([]byte, cookieLen)
	if _, err := rand.Read(b); err != nil {
		vlog.Errorf("newCookie failed: %v", err)
		return nil, nil
	}
	return &http.Cookie{
		Name:     cookieName,
		Value:    b64encode(b),
		Expires:  time.Now().Add(time.Hour * 24),
		HttpOnly: true,
	}, b
}

func decodeCookieValue(v string) ([]byte, error) {
	b, err := b64decode(v)
	if err != nil {
		return nil, err
	}
	if len(b) != cookieLen {
		return nil, fmt.Errorf("invalid cookie length[%d]", len(b))
	}
	return b, nil
}

// Shorthands.
func b64encode(b []byte) string          { return base64.URLEncoding.EncodeToString(b) }
func b64decode(s string) ([]byte, error) { return base64.URLEncoding.DecodeString(s) }
