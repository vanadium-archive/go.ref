package testutil

import (
	"io"
	"sort"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/naming"
)

// GlobName calls __Glob on the given object with the given pattern and returns
// a sorted list of matching object names, or an error.
func GlobName(ctx *context.T, name, pattern string) ([]string, error) {
	client := v23.GetClient(ctx)
	call, err := client.StartCall(ctx, name, ipc.GlobMethod, []interface{}{pattern})
	if err != nil {
		return nil, err
	}
	results := []string{}
Loop:
	for {
		var gr naming.VDLGlobReply
		switch err := call.Recv(&gr); err {
		case nil:
			switch v := gr.(type) {
			case naming.VDLGlobReplyEntry:
				results = append(results, v.Value.Name)
			}
		case io.EOF:
			break Loop
		default:
			return nil, err
		}
	}
	sort.Strings(results)
	if err := call.Finish(); err != nil {
		return nil, err
	}
	return results, nil
}
