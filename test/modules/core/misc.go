package core

import (
	"fmt"
	"io"
	"time"

	"v.io/x/ref/test/modules"
)

func init() {
	modules.RegisterFunction(SleepCommand, `[duration]
	sleep for a time(in go time.Duration format): defaults to 1s`, sleep)
	modules.RegisterFunction(TimeCommand, `
	prints the current time`, now)
}

func sleep(stdin io.Reader, stdout, stderr io.Writer, env map[string]string, args ...string) error {
	d := time.Second
	if len(args) > 1 {
		var err error
		if d, err = time.ParseDuration(args[1]); err != nil {
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
