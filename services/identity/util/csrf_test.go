package util

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFTokenWithoutCookie(t *testing.T) {
	cookieName := "VeyronCSRFTestCookie"
	r := newRequest()
	c := NewCSRFCop()
	w := httptest.NewRecorder()
	tok, err := c.NewToken(w, r, cookieName)
	if err != nil {
		t.Errorf("NewToken failed: %v", err)
	}
	cookie := cookieSet(w, cookieName)
	if len(cookie) == 0 {
		t.Errorf("Cookie should have been set. Request: [%v], Response: [%v]", r, w)
	}
	// Cookie needs to be present for validation
	r.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
	if err := c.ValidateToken(tok, r, cookieName); err != nil {
		t.Error("CSRF token failed validation:", err)
	}
	if err := c.ValidateToken(tok+"junk", r, cookieName); err == nil {
		t.Error("CSRF token should have failed validation")
	}
}

func TestCSRFTokenWithCookie(t *testing.T) {
	cookieName := "VeyronCSRFTestCookie"
	r := newRequest()
	c := NewCSRFCop()
	w := httptest.NewRecorder()
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "u776AC7hf794pTtGVlO50w=="})
	tok, err := c.NewToken(w, r, cookieName)
	if err != nil {
		t.Errorf("NewToken failed: %v", err)
	}
	if len(cookieSet(w, cookieName)) > 0 {
		t.Errorf("Cookie should not be set when it is already present. Request: [%v], Response: [%v]", r, w)
	}
	if err := c.ValidateToken(tok, r, cookieName); err != nil {
		t.Error("CSRF token failed validation:", err)
	}
	if err := c.ValidateToken(tok+"junk", r, cookieName); err == nil {
		t.Error("CSRF token should have failed validation")
	}
}

func cookieSet(w *httptest.ResponseRecorder, cookieName string) string {
	cookies := strings.Split(w.Header().Get("Set-Cookie"), ";")
	for _, c := range cookies {
		if strings.HasPrefix(c, cookieName) {
			return strings.TrimPrefix(c, cookieName+"=")
		}
	}
	return ""
}
