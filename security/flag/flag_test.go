package flag

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"testing"

	"v.io/v23/security"
	"v.io/v23/services/security/access"

	"v.io/core/veyron/lib/modules"
	tsecurity "v.io/core/veyron/lib/testutil/security"
)

//go:generate v23 test generate

var (
	acl1 = access.TaggedACLMap{}
	acl2 = access.TaggedACLMap{
		string(access.Read): access.ACL{
			In: []security.BlessingPattern{"veyron/alice/$", "veyron/bob"},
		},
		string(access.Write): access.ACL{
			In: []security.BlessingPattern{"veyron/alice/$"},
		},
	}

	expectedAuthorizer = map[string]security.Authorizer{
		"empty": auth(access.TaggedACLAuthorizer(acl1, access.TypicalTagType())),
		"acl2":  auth(access.TaggedACLAuthorizer(acl2, access.TypicalTagType())),
	}
)

func auth(a security.Authorizer, err error) security.Authorizer {
	if err != nil {
		panic(err)
	}
	return a
}

func tamFromFlag(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	nfargs := flag.CommandLine.Args()
	tam, err := TaggedACLMapFromFlag()
	if err != nil {
		fmt.Fprintf(stdout, "TaggedACLMapFromFlag() failed: %v", err)
		return nil
	}
	got := auth(access.TaggedACLAuthorizer(tam, access.TypicalTagType()))
	want := expectedAuthorizer[nfargs[0]]
	if !reflect.DeepEqual(got, want) {
		fmt.Fprintf(stdout, "args %#v\n", args)
		fmt.Fprintf(stdout, "AuthorizerFromFlags() got Authorizer: %v, want: %v", got, want)
	}
	return nil
}

func TestNewAuthorizerOrDie(t *testing.T) {
	sh, err := modules.NewShell(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)

	// Create a file.
	acl2FileName := tsecurity.SaveACLToFile(acl2)
	defer os.Remove(acl2FileName)

	testdata := []struct {
		cmd   string
		flags []string
		auth  string
	}{
		{
			cmd:   "tamFromFlag",
			flags: []string{"--veyron.acl.file", "runtime:" + acl2FileName},
			auth:  "acl2",
		},
		{
			cmd:   "tamFromFlag",
			flags: []string{"--veyron.acl.literal", "{}"},
			auth:  "empty",
		},
		{
			cmd:   "tamFromFlag",
			flags: []string{"--veyron.acl.literal", `{"Read": {"In":["veyron/alice/$", "veyron/bob"]}, "Write": {"In":["veyron/alice/$"]}}`},
			auth:  "acl2",
		},
	}

	for _, td := range testdata {
		fp := append(td.flags, td.auth)
		h, err := sh.Start(td.cmd, nil, fp...)
		if err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		b := new(bytes.Buffer)
		h.Shutdown(b, os.Stderr)
		if got := b.String(); got != "" {
			t.Errorf(got)
		}
	}
}
