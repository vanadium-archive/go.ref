package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
	"veyron.io/veyron/veyron2/security"
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
	if expected, got := "root/bob/... nin W\nroot/other in R\nroot/self/... in XRWADM", strings.TrimSpace(stdout.String()); got != expected {
		t.Fatalf("Unexpected output from get. Got %q, expected %q", got, expected)
	}
	if got, expected := tape.Play(), []interface{}{"GetACL"}; !reflect.DeepEqual(expected, got) {
		t.Errorf("invalid call sequence. Got %v, want %v", got, expected)
	}
	tape.Rewind()
	stdout.Reset()
}
