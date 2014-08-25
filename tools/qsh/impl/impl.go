package impl

import (
	"fmt"
	"io"
	"os"
	"sort"

	"veyron2/context"
	"veyron2/query"
	"veyron2/storage"
	"veyron2/storage/vstore"

	// TODO(rjkroege@google.com): Replace with the appropriate vom2 functionality
	// when available.
	_ "veyron/services/store/typeregistryhack"
)

func indenter(w io.Writer, indent int) {
	for i := 0; i < indent; i++ {
		fmt.Fprintf(w, "\t")
	}
}

// Prints a single QueryResult to the provided io.Writer.
func printResult(qr storage.QueryResult, w io.Writer, indent int) {
	// TODO(rjkroege@google.com): Consider permitting the user to provide a Go
	// template to format output.
	if v := qr.Value(); v != nil {
		indenter(w, indent)
		fmt.Fprintf(w, "%s: %#v\n", qr.Name(), v)
	} else {
		// Force fields to be consistently ordered.
		fields := qr.Fields()
		names := make([]string, 0, len(fields))
		for k, _ := range fields {
			names = append(names, k)
		}
		sort.Strings(names)

		indenter(w, indent)
		fmt.Fprintf(w, "%s: map[string]interface {}{\n", qr.Name())
		for _, k := range names {
			f := fields[k]
			switch v := f.(type) {
			case storage.QueryStream:
				indenter(w, indent+1)
				fmt.Fprintf(w, "%s: {\n", k)
				printStream(v, w, indent+2)
				indenter(w, indent+1)
				fmt.Fprintf(w, "},\n")
			default:
				indenter(w, indent+1)
				fmt.Fprintf(w, "\"%s\":%#v,\n", k, v)
			}
		}
		indenter(w, indent)
		fmt.Fprintf(w, "},\n")
	}
}

func printStream(qs storage.QueryStream, w io.Writer, indent int) error {
	for qs.Advance() {
		printResult(qs.Value(), w, indent)
	}
	if err := qs.Err(); err != nil {
		return err
	}
	return nil
}

func RunQuery(ctx context.T, queryRoot, queryString string) error {
	return printStream(vstore.New().Bind(queryRoot).Query(ctx, query.Query{queryString}), os.Stdout, 0)
}
