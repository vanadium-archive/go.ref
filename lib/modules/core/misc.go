package core

import (
	"fmt"
	"io"
	"time"

	"veyron.io/veyron/veyron2/naming"
	"veyron.io/veyron/veyron2/rt"

	"veyron.io/veyron/veyron/lib/modules"
)

func sleep(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	d := time.Second
	if len(args) > 0 {
		var err error
		if d, err = time.ParseDuration(args[0]); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Sleeping for %s\n", d)
	eof := make(chan struct{})
	go func() {
		modules.WaitForEOF(stdin)
		close(eof)
	}()

	then := time.Now()
	select {
	case <-time.After(d):
		fmt.Fprintf(stdout, "Slept for %s\n", time.Now().Sub(then))
	case <-eof:
		fmt.Fprintf(stdout, "Aborted after %s\n", time.Now().Sub(then))
	}
	return nil
}

func now(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	fmt.Fprintf(stdout, "%s\n", time.Now())
	return nil
}

func mountServer(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) != 3 {
		return fmt.Errorf("wrong # args")
	}
	mp, server, ttlstr := args[0], args[1], args[2]
	ttl, err := time.ParseDuration(ttlstr)
	if err != nil {
		return fmt.Errorf("failed to parse time from %q", ttlstr)
	}
	ns := rt.R().Namespace()
	if err := ns.Mount(rt.R().NewContext(), mp, server, ttl); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Mount(%s, %s, %s)\n", mp, server, ttl)
	return nil
}

func namespaceCache(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	if len(args) != 1 {
		return fmt.Errorf("wrong # args")
	}
	disable := true
	switch args[0] {
	case "on":
		disable = false
	case "off":
		disable = true
	default:
		return fmt.Errorf("arg must be 'on' or 'off'")
	}
	rt.R().Namespace().CacheCtl(naming.DisableCache(disable))
	return nil
}
