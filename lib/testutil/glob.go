package testutil

import (
	"io"
	"sort"

	"v.io/core/veyron2"
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/naming"
)

// GlobName calls __Glob on the given object with the given pattern and returns
// a sorted list of matching object names, or an error.
func GlobName(ctx *context.T, name, pattern string) ([]string, error) {
	client := veyron2.GetClient(ctx)
	call, err := client.StartCall(ctx, name, ipc.GlobMethod, []interface{}{pattern})
	if err != nil {
		return nil, err
	}
	results := []string{}
Loop:
	for {
		var me naming.VDLMountEntry
		switch err := call.Recv(&me); err {
		case nil:
			results = append(results, me.Name)
		case io.EOF:
			break Loop
		default:
			return nil, err
		}
	}
	sort.Strings(results)
	if ferr := call.Finish(&err); ferr != nil {
		err = ferr
	}

	return results, err
}
