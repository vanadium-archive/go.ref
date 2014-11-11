package testutil

import (
	"io"
	"sort"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"
)

// GlobName calls __Glob on the given object with the given pattern and returns
// a sorted list of matching object names, or an error.
func GlobName(name, pattern string) ([]string, error) {
	call, err := rt.R().Client().StartCall(rt.R().NewContext(), name, ipc.GlobMethod, []interface{}{pattern})
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
