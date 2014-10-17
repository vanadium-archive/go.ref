package auditor

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"testing"
	"time"

	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"

	_ "veyron.io/veyron/veyron/profiles"
	"veyron.io/veyron/veyron/security/audit"
)

func TestFileAuditor(t *testing.T) {
	r := rt.Init()
	defer r.Cleanup()

	// Setup a directory where the audit logs will be stored.
	dir, err := ioutil.TempDir("", "TestFileAuditor")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	prefix := path.Join(dir, "log")
	auditor, err := NewFileAuditor(prefix)
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := r.NewIdentity("provider")
	provider = audit.NewPrivateID(provider, auditor)
	blessee := func() security.PublicID {
		priv, _ := r.NewIdentity("blessee")
		return priv.PublicID()
	}()

	// Bless a bunch of names
	for idx, name := range []string{"brucewayne", "batman", "bugsbunny", "daffyduck", "batman", "bugsbunny", "brucewayne"} {
		if _, err := provider.Bless(blessee, name, time.Hour, nil); err != nil {
			t.Fatalf("Bless(#%d, %v, ...) failed: %v", idx, name, err)
		}
	}

	tests := []struct {
		Filter string
		Want   []string
	}{
		{"", []string{"brucewayne", "batman", "bugsbunny", "daffyduck", "batman", "bugsbunny", "brucewayne"}},
		{"/", []string{"brucewayne", "batman", "bugsbunny", "daffyduck", "batman", "bugsbunny", "brucewayne"}},
		{"batman", []string{"batman", "batman"}},
		{"daffyduck", []string{"daffyduck"}},
		{"elmerfudd", []string{}},
	}
	for _, test := range tests {
		ch, err := ReadAuditLog(prefix, test.Filter)
		if err != nil {
			t.Errorf("ReadAuditLog(%q, %q) failed: %v", prefix, test.Filter, err)
			continue
		}
	channelloop:
		for i, want := range test.Want {
			got := <-ch
			testinfo := fmt.Sprintf("in test %+v (%d/%d)", test, i, len(want))
			if got.Method != "Bless" {
				t.Errorf("Got %q, wanted the Bless method %s", got, testinfo)
				break channelloop
			}
			if arg := got.Arguments[0]; !reflect.DeepEqual(arg, blessee) {
				t.Errorf("First argument was %v, wanted %v", arg, blessee, testinfo)
				break channelloop
			}
			if arg := got.Arguments[1].(string); arg != want {
				t.Errorf("Second argument was %v, wanted %v %s", arg, want, testinfo)
				break channelloop
			}
		}
		select {
		case entry, open := <-ch:
			if open {
				for _ = range ch {
					// Drain the channel to prevent the producer goroutines from being leaked.
				}
				t.Errorf("Got more entries that expected (such as %v) for test %+v", entry, test)
			}
		default:
		}
	}
}
