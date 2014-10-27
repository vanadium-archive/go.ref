package core

import (
	"flag"
	"fmt"

	"veyron.io/veyron/veyron2/ipc"

	"veyron.io/veyron/veyron/lib/flags"
)

// ParseCommonFlags parses the supplied args for the common set of flags
// and environment variables defined in in the veyron/lib/flags package.
func ParseCommonFlags(args []string) (*flags.Flags, error) {
	fs := flag.NewFlagSet("modules/core", flag.ContinueOnError)
	fl := flags.New(fs)
	if len(args) == 0 {
		return fl, fmt.Errorf("no args supplied")
	}
	err := fl.Parse(args[1:])
	return fl, err
}

func initListenSpec(fl *flags.Flags) ipc.ListenSpec {
	return ipc.ListenSpec{
		Protocol: fl.ListenProtocolFlag.String(),
		Address:  fl.ListenAddressFlag.String(),
		Proxy:    fl.ListenProxyFlag,
	}
}

// checkArgs checks for the expected number of args in args. A -ve
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
