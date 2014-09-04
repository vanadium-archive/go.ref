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

	"veyron2/vlog"
)

const cookieLen = 16

type CSRFCop struct{}

func NewCSRFCop() *CSRFCop { return new(CSRFCop) }

func (*CSRFCop) maybeSetCookie(w http.ResponseWriter, req *http.Request, cookieName string) ([]byte, error) {
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
	return v, nil
}

// NewToken creates an anti-cross-site-request-forgery, aka CSRF aka XSRF token.
// It returns an error if the token could not be created.
func (c *CSRFCop) NewToken(w http.ResponseWriter, r *http.Request, cookieName string) (string, error) {
	cookie, err := c.maybeSetCookie(w, r, cookieName)
	if err != nil {
		return "", fmt.Errorf("bad cookie: %v", err)
	}
	return c.newToken(cookie), nil
}

func (c *CSRFCop) newToken(cookie []byte) string {
	return b64encode(hmac.New(sha256.New, cookie).Sum(nil))
}

// ValidateToken checks the validity of the provided CSRF token for the
// provided request, returning nil if the token is valid and an error
// otherwise.
// The returned error should not be shown to end-users, it is meant for
// consumption of the server process only.
func (c *CSRFCop) ValidateToken(token string, req *http.Request, cookieName string) error {
	cookie, err := req.Cookie(cookieName)
	if err != nil {
		return err
	}
	cookieValue, err := decodeCookieValue(cookie.Value)
	if err != nil {
		return fmt.Errorf("invalid cookie")
	}
	if token != c.newToken(cookieValue) {
		return fmt.Errorf("invalid CSRF token")
	}
	return nil
}

func requestString(r *http.Request) string {
	var buf bytes.Buffer
	r.Write(&buf)
	return buf.String()
}

type badRequestData struct {
	Request string
	Error   error
}

func newCookie(cookieName string) (*http.Cookie, []byte) {
	b := make([]byte, cookieLen)
	if n, err := rand.Read(b); n != cookieLen || err != nil {
		vlog.Errorf("newCookie failed: Read %d random bytes, wanted %d. err: %v", n, cookieLen, err)
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
