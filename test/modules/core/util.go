package core

import (
	"flag"
	"fmt"
	"strings"
)

// checkArgs checks for the expected number of args in args. A negative
// value means at least that number of args are expected.
func checkArgs(args []string, expected int, usage string) error {
	got := len(args)
	if expected < 0 {
		expected = -expected
		if got < expected {
			return fmt.Errorf("wrong # args (got %d, expected >=%d) expected: %q got: %v", got, expected, usage, args)
		}
	} else {
		if got != expected {
			return fmt.Errorf("wrong # args (got %d, expected %d) expected: %q got: %v", got, expected, usage, args)
		}
	}
	return nil
}

// usage generates a usage string based on the flags in a flagset.
func usage(fs *flag.FlagSet) string {
	res := []string{}
	fs.VisitAll(func(f *flag.Flag) {
		format := "  -%s=%s: %s"
		if getter, ok := f.Value.(flag.Getter); ok {
			if _, ok := getter.Get().(string); ok {
				// put quotes on the value
				format = "  -%s=%q: %s"
			}
		}
		res = append(res, fmt.Sprintf(format, f.Name, f.DefValue, f.Usage))
	})
	return strings.Join(res, "\n") + "\n"
}
