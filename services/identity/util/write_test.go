package util

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSend(t *testing.T) {
	impl := impl{Content: "batman"}
	var iface iface
	iface = &impl

	tests := []interface{}{
		1,
		"foo",
		iface,
		impl,
	}
	for _, test := range tests {
		w := httptest.NewRecorder()
		HTTPSend(w, test)
		if got, want := w.Code, http.StatusOK; got != want {
			t.Errorf("Got %d want %d (%T=%#v)", got, want, test, test)
			continue
		}
		if got, want := w.HeaderMap.Get("Content-Type"), "text/plain"; got != want {
			t.Errorf("Got %v want %v (%T=%v)", got, want, test, test)
		}
		var decoded interface{}
		if err := Base64VomDecode(w.Body.String(), &decoded); err != nil {
			t.Errorf("%v (%T=%v)", err, test, test)
		}
	}
}

func TestBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	HTTPBadRequest(w, newRequest(), nil)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Errorf("Got %d, want %d", got, want)
	}
}

func TestServerError(t *testing.T) {
	w := httptest.NewRecorder()
	HTTPServerError(w, nil)
	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Errorf("Got %d, want %d", got, want)
	}
}
