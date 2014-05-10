package util

import (
	"reflect"
	"testing"
)

func TestCoder(t *testing.T) {
	var iface iface
	impl := &impl{}
	iface = impl
	tests := []interface{}{
		1,
		"string",
		impl,
		iface,
	}
	for _, item := range tests {
		b64, err := Base64VomEncode(item)
		if err != nil {
			t.Errorf("Failed to encode %T=%#v: %v", item, item, err)
			continue
		}
		var decoded interface{}
		if err = Base64VomDecode(b64, &decoded); err != nil {
			t.Errorf("Failed to decode %T=%#v: %v", item, item, err)
			continue
		}
		if !reflect.DeepEqual(decoded, item) {
			t.Errorf("Got (%T, %#v) want (%T, %#v)", decoded, decoded, item, item)
		}
	}
}
