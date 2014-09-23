package util

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	cookieName     = "VeyronCSRFTestCookie"
	failCookieName = "FailCookieName"
)

func TestCSRFTokenWithoutCookie(t *testing.T) {
	r := newRequest()
	c, err := NewCSRFCop()
	if err != nil {
		t.Fatalf("NewCSRFCop() failed: %v", err)
	}
	w := httptest.NewRecorder()
	tok, err := c.NewToken(w, r, cookieName, nil)
	if err != nil {
		t.Errorf("NewToken failed: %v", err)
	}
	cookie := cookieSet(w, cookieName)
	if len(cookie) == 0 {
		t.Errorf("Cookie should have been set. Request: [%v], Response: [%v]", r, w)
	}
	// Cookie needs to be present for validation
	r.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
	if err := c.ValidateToken(tok, r, cookieName, nil); err != nil {
		t.Error("CSRF token failed validation:", err)
	}

	w = httptest.NewRecorder()
	if _, err = c.MaybeSetCookie(w, r, failCookieName); err != nil {
		t.Error("failed to create cookie: ", err)
	}
	cookie = cookieSet(w, failCookieName)
	if len(cookie) == 0 {
		t.Errorf("Cookie should have been set. Request: [%v], Response: [%v]", r, w)
	}

	if err := c.ValidateToken(tok, r, failCookieName, nil); err == nil {
		t.Error("CSRF token should have failed validation")
	}
}

func TestCSRFTokenWithCookie(t *testing.T) {
	r := newRequest()
	c, err := NewCSRFCop()
	if err != nil {
		t.Fatalf("NewCSRFCop() failed: %v", err)
	}
	w := httptest.NewRecorder()
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "u776AC7hf794pTtGVlO50w=="})
	tok, err := c.NewToken(w, r, cookieName, nil)
	if err != nil {
		t.Errorf("NewToken failed: %v", err)
	}
	if len(cookieSet(w, cookieName)) > 0 {
		t.Errorf("Cookie should not be set when it is already present. Request: [%v], Response: [%v]", r, w)
	}
	if err := c.ValidateToken(tok, r, cookieName, nil); err != nil {
		t.Error("CSRF token failed validation:", err)
	}

	r.AddCookie(&http.Cookie{Name: failCookieName, Value: "u864AC7gf794pTtCAlO40w=="})
	if err := c.ValidateToken(tok, r, failCookieName, nil); err == nil {
		t.Error("CSRF token should have failed validation")
	}
}

func TestCSRFTokenWithData(t *testing.T) {
	r := newRequest()
	c, err := NewCSRFCop()
	if err != nil {
		t.Fatalf("NewCSRFCop() failed: %v", err)
	}
	w := httptest.NewRecorder()
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "u776AC7hf794pTtGVlO50w=="})
	tok, err := c.NewToken(w, r, cookieName, 1)
	if err != nil {
		t.Errorf("NewToken failed: %v", err)
	}
	if len(cookieSet(w, cookieName)) > 0 {
		t.Errorf("Cookie should not be set when it is already present. Request: [%v], Response: [%v]", r, w)
	}
	var got int
	if err := c.ValidateToken(tok, r, cookieName, &got); err != nil {
		t.Error("CSRF token failed validation:", err)
	}
	if want := 1; got != want {
		t.Errorf("Got %v, want %v", got, want)
	}

	r.AddCookie(&http.Cookie{Name: failCookieName, Value: "u864AC7gf794pTtCAlO40w=="})
	if err := c.ValidateToken(tok, r, failCookieName, &got); err == nil {
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
