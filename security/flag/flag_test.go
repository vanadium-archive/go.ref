package flag

import (
	"flag"
	"os"
	"reflect"
	"testing"

	tsecurity "v.io/veyron/veyron/lib/testutil/security"

	"v.io/veyron/veyron2/security"
	"v.io/veyron/veyron2/services/security/access"
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
		acl1 = access.TaggedACLMap{}
		acl2 = access.TaggedACLMap{
			string(access.Read): access.ACL{
				In: []security.BlessingPattern{"veyron/alice", "veyron/bob"},
			},
			string(access.Write): access.ACL{
				In: []security.BlessingPattern{"veyron/alice"},
			},
		}

		auth = func(a security.Authorizer, err error) security.Authorizer {
			if err != nil {
				panic(err)
			}
			return a
		}
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
			wantAuth: auth(access.TaggedACLAuthorizer(acl1, access.TypicalTagType())),
		},
		{
			flags:    flagValue{"acl": `{"In":{"veyron/alice":"RW", "veyron/bob": "R"}}`},
			wantAuth: auth(access.TaggedACLAuthorizer(acl2, access.TypicalTagType())),
		},
		{
			flags:    flagValue{"acl": `{"In":{"veyron/bob":"R", "veyron/alice": "WR"}}`},
			wantAuth: auth(access.TaggedACLAuthorizer(acl2, access.TypicalTagType())),
		},
		{
			flags:    flagValue{"acl_file": acl2File},
			wantAuth: auth(access.TaggedACLAuthorizerFromFile(acl2File, access.TypicalTagType())),
		},
		{
			flags:     flagValue{"acl_file": acl2File, "acl": `{"In":{"veyron/alice":"RW", "veyron/bob": "R"}}`},
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
