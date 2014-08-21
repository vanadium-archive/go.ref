package flag

import (
	"flag"
	"os"
	"reflect"
	"testing"

	tsecurity "veyron/lib/testutil/security"
	vsecurity "veyron/security"

	"veyron2/security"
)

func TestNewAuthorizerOrDie(t *testing.T) {
	type flagValue map[string]string
	testNewAuthorizerOrDie := func(flags flagValue, wantAuth security.Authorizer, wantPanic bool) {
		defer func() {
			if gotPanic := (recover() != nil); wantPanic != gotPanic {
				t.Errorf("AuthorizerFromFlags() with flags %v, got panic: %v, want panic: %v ", flags, gotPanic, wantPanic)
			}
		}()
		if got := NewAuthorizerOrDie(); !reflect.DeepEqual(got, wantAuth) {
			t.Errorf("AuthorizerFromFlags() with flags %v: got Authorizer: %v, want: %v", flags, got, wantAuth)
		}
	}
	clearACLFlags := func() {
		flag.Set("acl", "")
		flag.Set("acl_file", "")
	}
	var (
		acl1 = security.ACL{}
		acl2 = vsecurity.NewWhitelistACL(map[security.PrincipalPattern]security.LabelSet{
			"veyron/alice": security.LabelSet(security.ReadLabel | security.WriteLabel),
			"veyron/bob":   security.LabelSet(security.ReadLabel),
		})
	)
	acl2File := tsecurity.SaveACLToFile(acl2)
	defer os.Remove(acl2File)

	testdata := []struct {
		flags     flagValue
		wantAuth  security.Authorizer
		wantPanic bool
	}{
		{
			flags:    flagValue{},
			wantAuth: nil,
		},
		{
			flags:    flagValue{"acl": "{}"},
			wantAuth: vsecurity.NewACLAuthorizer(acl1),
		},
		{
			flags:    flagValue{"acl": "{\"In\":{\"Principals\":{\"veyron/alice\":\"RW\", \"veyron/bob\": \"R\"}}}"},
			wantAuth: vsecurity.NewACLAuthorizer(acl2),
		},
		{
			flags:    flagValue{"acl": "{\"In\":{\"Principals\":{\"veyron/bob\":\"R\", \"veyron/alice\": \"WR\"}}}"},
			wantAuth: vsecurity.NewACLAuthorizer(acl2),
		},
		{
			flags:    flagValue{"acl_file": acl2File},
			wantAuth: vsecurity.NewFileACLAuthorizer(acl2File),
		},
		{
			flags:     flagValue{"acl_file": acl2File, "acl": "{\"In\":{\"Principals\":{\"veyron/alice\":\"RW\", \"veyron/bob\": \"R\"}}}"},
			wantPanic: true,
		},
	}
	for _, d := range testdata {
		clearACLFlags()
		for f, v := range d.flags {
			flag.Set(f, v)
		}
		testNewAuthorizerOrDie(d.flags, d.wantAuth, d.wantPanic)
	}
}
