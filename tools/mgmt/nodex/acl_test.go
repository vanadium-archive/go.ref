package main

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
	"veyron.io/veyron/veyron2/services/security/access"
	"veyron.io/veyron/veyron2/verror"
)

func TestACLGetCommand(t *testing.T) {
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	nodeName := naming.JoinAddressName(endpoint.String(), "")

	// Test the 'list' command.
	tape.SetResponses([]interface{}{GetACLResponse{
		acl: security.ACL{
			In: map[security.BlessingPattern]security.LabelSet{
				"root/self/...": security.AllLabels,
				"root/other":    security.LabelSet(security.ReadLabel),
			},
			NotIn: map[string]security.LabelSet{
				"root/bob/...": security.LabelSet(security.WriteLabel),
			},
		},
		etag: "anEtagForToday",
		err:  nil,
	},
	})

	if err := cmd.Execute([]string{"acl", "get", nodeName}); err != nil {
		t.Fatalf("%v, ouput: %v, error: %v", err)
	}
	if expected, got := "root/bob/... !W\nroot/other R\nroot/self/... XRWADM", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from get. Got %q, expected %q", got, expected)
	}
	if got, expected := tape.Play(), []interface{}{"GetACL"}; !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}

func TestACLSetCommand(t *testing.T) {
	runtime := rt.Init()
	tape := NewTape()
	server, endpoint, err := startServer(t, runtime, tape)
	if err != nil {
		return
	}
	defer stopServer(t, server)

	// Setup the command-line.
	cmd := root()
	var stdout, stderr bytes.Buffer
	cmd.Init(nil, &stdout, &stderr)
	nodeName := naming.JoinAddressName(endpoint.String(), "")

	// Some tests to validate parse.
	if err := cmd.Execute([]string{"acl", "set", nodeName}); err == nil {
		t.Fatalf("failed to correctly detect insufficient parameters")
	}
	if expected, got := "ERROR: set: incorrect number of arguments 1, must be 1 + 2n", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from list. Got %q, expected prefix %q", got, expected)
	}

	stderr.Reset()
	stdout.Reset()
	if err := cmd.Execute([]string{"acl", "set", nodeName, "foo"}); err == nil {
		t.Fatalf("failed to correctly detect insufficient parameters")
	}
	if expected, got := "ERROR: set: incorrect number of arguments 2, must be 1 + 2n", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from list. Got %q, expected prefix %q", got, expected)
	}

	stderr.Reset()
	stdout.Reset()
	if err := cmd.Execute([]string{"acl", "set", nodeName, "foo", "bar", "ohno"}); err == nil {
		t.Fatalf("failed to correctly detect insufficient parameters")
	}
	if expected, got := "ERROR: set: incorrect number of arguments 4, must be 1 + 2n", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from list. Got %q, expected prefix %q", got, expected)
	}

	stderr.Reset()
	stdout.Reset()
	if err := cmd.Execute([]string{"acl", "set", nodeName, "foo", "!"}); err == nil {
		t.Fatalf("failed to detect invalid parameter")
	}
	if expected, got := "ERROR: failed to parse LabelSet pair foo, !", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from list. Got %q, expected prefix %q", got, expected)
	}

	// Correct operation in the absence of errors.
	stderr.Reset()
	stdout.Reset()
	tape.SetResponses([]interface{}{GetACLResponse{
		acl: security.ACL{
			In: map[security.BlessingPattern]security.LabelSet{
				"root/self/...": security.AllLabels,
				"root/other":    security.LabelSet(security.ReadLabel),
			},
			NotIn: map[string]security.LabelSet{
				"root/bob": security.LabelSet(security.WriteLabel),
			},
		},
		etag: "anEtagForToday",
		err:  nil,
	},
		verror.Make(access.ErrBadEtag, fmt.Sprintf("etag mismatch in:%s vers:%s", "anEtagForToday", "anEtagForTomorrow")),
		GetACLResponse{
			acl: security.ACL{
				In: map[security.BlessingPattern]security.LabelSet{
					"root/self/...": security.AllLabels,
					"root/other":    security.LabelSet(security.ReadLabel),
				},
				NotIn: map[string]security.LabelSet{
					"root/bob":       security.LabelSet(security.WriteLabel),
					"root/alice/cat": security.LabelSet(security.AdminLabel),
				},
			},
			etag: "anEtagForTomorrow",
			err:  nil,
		},
		nil,
	})

	if err := cmd.Execute([]string{"acl", "set", nodeName, "root/vana/bad", "!XR", "root/vana/...", "RD", "root/other", "0", "root/bob", "!0"}); err != nil {
		t.Fatalf("SetACL failed: %v", err)
	}

	if expected, got := "", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	if expected, got := "WARNING: trying again because of asynchronous change", strings.TrimSpace(stderr.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}

	expected := []interface{}{
		"GetACL",
		SetACLStimulus{
			fun: "SetACL",
			acl: security.ACL{
				In: map[security.BlessingPattern]security.LabelSet{
					"root/self/...": security.AllLabels,
					"root/vana/...": security.LabelSet(security.ReadLabel | security.DebugLabel),
				},
				NotIn: map[string]security.LabelSet{
					"root/vana/bad": security.LabelSet(security.ResolveLabel | security.ReadLabel),
				},
			},
			etag: "anEtagForToday",
		},
		"GetACL",
		SetACLStimulus{
			fun: "SetACL",
			acl: security.ACL{
				In: map[security.BlessingPattern]security.LabelSet{
					"root/self/...": security.AllLabels,
					"root/vana/...": security.LabelSet(security.ReadLabel | security.DebugLabel),
				},
				NotIn: map[string]security.LabelSet{
					"root/alice/cat": security.LabelSet(security.AdminLabel),
					"root/vana/bad":  security.LabelSet(security.ResolveLabel | security.ReadLabel),
				},
			},
			etag: "anEtagForTomorrow",
		},
	}

	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()

	// GetACL fails.
	tape.SetResponses([]interface{}{GetACLResponse{
		acl:  security.ACL{In: nil, NotIn: nil},
		etag: "anEtagForToday",
		err:  verror.BadArgf("oops!"),
	},
	})

	if err := cmd.Execute([]string{"acl", "set", nodeName, "root/vana/bad", "!XR", "root/vana/...", "RD", "root/other", "0", "root/bob", "!0"}); err == nil {
		t.Fatalf("GetACL RPC inside acl set command failed but error wrongly not detected")
	}
	if expected, got := "ERROR: GetACL("+nodeName+") failed: oops!", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from list. Got %q, prefix %q", got, expected)
	}
	if expected, got := "", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	expected = []interface{}{
		"GetACL",
	}

	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()

	// SetACL fails with not a bad etag failure.
	tape.SetResponses([]interface{}{GetACLResponse{
		acl: security.ACL{
			In: map[security.BlessingPattern]security.LabelSet{
				"root/self/...": security.AllLabels,
				"root/other":    security.LabelSet(security.ReadLabel),
			},
			NotIn: map[string]security.LabelSet{
				"root/bob": security.LabelSet(security.WriteLabel),
			},
		},
		etag: "anEtagForToday",
		err:  nil,
	},
		verror.BadArgf("oops!"),
	})

	if err := cmd.Execute([]string{"acl", "set", nodeName, "root/vana/bad", "!XR", "root/vana/...", "RD", "root/other", "0", "root/bob", "!0"}); err == nil {
		t.Fatalf("SetACL should have failed: %v", err)
	}
	if expected, got := "", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	if expected, got := "ERROR: SetACL("+nodeName+") failed: oops!", strings.TrimSpace(stderr.String()); !strings.HasPrefix(got, expected) {
		t.Fatalf("Unexpected output from list. Got %q, prefix %q", got, expected)
	}

	expected = []interface{}{
		"GetACL",
		SetACLStimulus{
			fun: "SetACL",
			acl: security.ACL{
				In: map[security.BlessingPattern]security.LabelSet{
					"root/self/...": security.AllLabels,
					"root/vana/...": security.LabelSet(security.ReadLabel | security.DebugLabel),
				},
				NotIn: map[string]security.LabelSet{
					"root/vana/bad": security.LabelSet(security.ResolveLabel | security.ReadLabel),
				},
			},
			etag: "anEtagForToday",
		},
	}

	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()

	// Trying to delete non-existent items.
	stderr.Reset()
	stdout.Reset()
	tape.SetResponses([]interface{}{GetACLResponse{
		acl: security.ACL{
			In:    map[security.BlessingPattern]security.LabelSet{},
			NotIn: map[string]security.LabelSet{},
		},
		etag: "anEtagForToday",
		err:  nil,
	},
		nil,
	})

	if err := cmd.Execute([]string{"acl", "set", nodeName, "root/vana/notin/missing", "!0", "root/vana/in/missing", "0"}); err != nil {
		t.Fatalf("SetACL failed: %v", err)
	}
	if expected, got := "", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}
	if expected, got := "WARNING: ignoring attempt to remove non-existing NotIn ACL for root/vana/notin/missing\nWARNING: ignoring attempt to remove non-existing In ACL for root/vana/in/missing", strings.TrimSpace(stderr.String()); got != expected {
		t.Fatalf("Unexpected output from list. Got %q, expected %q", got, expected)
	}

	expected = []interface{}{
		"GetACL",
		SetACLStimulus{
			fun:  "SetACL",
			acl:  security.ACL{},
			etag: "anEtagForToday",
		},
	}
	if got := tape.Play(); !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
	stderr.Reset()
}
