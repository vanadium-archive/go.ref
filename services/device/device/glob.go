// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"v.io/x/lib/cmdline"
	deviceimpl "v.io/x/ref/services/device/internal/impl"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/services/device"
	"v.io/v23/verror"

	"v.io/x/ref/lib/v23cmd"
)

// globHandler is implemented by each command that wants to execute against name
// patterns.  The handler is expected to be invoked against each glob result,
// and can be run concurrently. The handler should direct its output to the
// given stdout and stderr writers.
//
// Typical usage:
//
// func myCmdHandler(entry globResult, stdout, stderr io.Writer) error {
//   output := myCmdProcessing(entry)
//   fmt.Fprintf(stdout, output)
//   ...
// }
//
// func runMyCmd(ctx *context.T, env *cmdline.Env, args []string) error {
//   ...
//   err := run(ctx, env, args, myCmdHandler)
//   ...
// }
//
// var cmdMyCmd = &cmdline.Command {
//   Runner: v23cmd.RunnerFunc(runMyCmd)
//   ...
// }
//
// Alternatively, if all runMyCmd does is to call run, you can use globRunner to
// avoid having to define runMyCmd:
//
// var cmdMyCmd = &cmdline.Command {
//   Runner: globRunner(myCmdHandler)
//   ...
// }
type globHandler func(entry globResult, stdout, stderr io.Writer) error

func globRunner(handler globHandler) cmdline.Runner {
	return v23cmd.RunnerFunc(func(ctx *context.T, env *cmdline.Env, args []string) error {
		return run(ctx, env, args, handler)
	})
}

type globResult struct {
	name       string
	isInstance bool
	status     device.Status
}

type byTypeAndName []globResult

func (a byTypeAndName) Len() int      { return len(a) }
func (a byTypeAndName) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

func (a byTypeAndName) Less(i, j int) bool {
	r1, r2 := a[i], a[j]
	if r1.isInstance {
		if !r2.isInstance {
			return false
		}
	} else if r2.isInstance {
		return true
	}
	return r1.name < r2.name
}

// TODO(caprita): Allow clients to control the parallelism.  For example, for
// updateall, we need to first update the installations before we update the
// instances.

// run runs the given handler in parallel against each of the results obtained
// by globbing args, after performing filtering based on type
// (instance/installation) and state.  No de-duping of results is performed.
// The outputs from each of the handlers are sorted: installations first, then
// instances (and alphabetically by object name for each group).
func run(ctx *context.T, env *cmdline.Env, args []string, handler globHandler) error {
	results := glob(ctx, env, args)
	sort.Sort(byTypeAndName(results))
	stdouts, stderrs := make([]bytes.Buffer, len(results)), make([]bytes.Buffer, len(results))
	var wg sync.WaitGroup
	errors := make(chan struct {
		name string
		err  error
	}, len(results))
	for i, r := range results {
		switch s := r.status.(type) {
		case device.StatusInstance:
			if onlyInstallations || !instanceStateFilter.apply(s.Value.State) {
				continue
			}
		case device.StatusInstallation:
			if onlyInstances || !installationStateFilter.apply(s.Value.State) {
				continue
			}
		}
		wg.Add(1)
		go func(r globResult, index int) {
			errors <- struct {
				name string
				err  error
			}{r.name, handler(r, &stdouts[index], &stderrs[index])}
			wg.Done()
		}(r, i)
	}
	wg.Wait()
	for i := range results {
		io.Copy(env.Stdout, &stdouts[i])
		io.Copy(env.Stderr, &stderrs[i])
	}
	close(errors)
	nErrors := 0
	for e := range errors {
		if e.err != nil {
			fmt.Fprintf(env.Stderr, "ERROR for %q: %v", e.name, e.err)
			nErrors++
		}
	}
	if nErrors > 0 {
		return fmt.Errorf("encountered a total of %d errors", nErrors)
	}
	return nil
}

// TODO(caprita): We need to filter out debug objects under the app instances'
// namespaces, so that the tool works with ... patterns.  We should change glob
// on device manager to not return debug objects by default for apps and instead
// put them under a __debug suffix (like it works for services).
var debugNameRE = regexp.MustCompile("/apps/[^/]+/[^/]+/[^/]+/(logs|stats|pprof)(/|$)")

func getStatus(ctx *context.T, env *cmdline.Env, name string, resultsCh chan<- globResult) {
	status, err := device.DeviceClient(name).Status(ctx)
	// Skip non-instances/installations.
	if verror.ErrorID(err) == deviceimpl.ErrInvalidSuffix.ID {
		return
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "Status(%v) failed: %v\n", name, err)
		return
	}
	_, isInstance := status.(device.StatusInstance)
	resultsCh <- globResult{name, isInstance, status}
}

func globOne(ctx *context.T, env *cmdline.Env, pattern string, resultsCh chan<- globResult) {
	globCh, err := v23.GetNamespace(ctx).Glob(ctx, pattern)
	if err != nil {
		fmt.Fprintf(env.Stderr, "Glob(%v) failed: %v\n", pattern, err)
		return
	}
	var wg sync.WaitGroup
	// For each glob result.
	for entry := range globCh {
		switch v := entry.(type) {
		case *naming.GlobReplyEntry:
			name := v.Value.Name
			// Skip debug objects.
			if debugNameRE.MatchString(name) {
				continue
			}
			wg.Add(1)
			go func() {
				getStatus(ctx, env, name, resultsCh)
				wg.Done()
			}()
		case *naming.GlobReplyError:
			fmt.Fprintf(env.Stderr, "Glob(%v) returned an error for %v: %v\n", pattern, v.Value.Name, v.Value.Error)
		}
	}
	wg.Wait()
}

// glob globs the given arguments and returns the results in arbitrary order.
func glob(ctx *context.T, env *cmdline.Env, args []string) []globResult {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var wg sync.WaitGroup
	resultsCh := make(chan globResult, 100)
	// For each pattern.
	for _, a := range args {
		wg.Add(1)
		go func(pattern string) {
			globOne(ctx, env, pattern, resultsCh)
			wg.Done()
		}(a)
	}
	// Collect the glob results into a slice.
	var results []globResult
	done := make(chan struct{})
	go func() {
		for r := range resultsCh {
			results = append(results, r)
		}
		close(done)
	}()
	wg.Wait()
	close(resultsCh)
	<-done
	return results
}

type instanceStateFlag map[device.InstanceState]struct{}

func (f *instanceStateFlag) apply(state device.InstanceState) bool {
	if len(*f) == 0 {
		return true
	}
	_, ok := (*f)[state]
	return ok
}

func (f *instanceStateFlag) String() string {
	states := make([]string, 0, len(*f))
	for s := range *f {
		states = append(states, s.String())
	}
	sort.Strings(states)
	return strings.Join(states, ",")
}

func (f *instanceStateFlag) Set(s string) error {
	if *f == nil {
		*f = make(instanceStateFlag)
	}
	states := strings.Split(s, ",")
	for _, s := range states {
		state, err := device.InstanceStateFromString(s)
		if err != nil {
			return err
		}
		(*f)[state] = struct{}{}
	}
	return nil
}

type installationStateFlag map[device.InstallationState]struct{}

func (f *installationStateFlag) apply(state device.InstallationState) bool {
	if len(*f) == 0 {
		return true
	}
	_, ok := (*f)[state]
	return ok
}

func (f *installationStateFlag) String() string {
	states := make([]string, 0, len(*f))
	for s := range *f {
		states = append(states, s.String())
	}
	sort.Strings(states)
	return strings.Join(states, ",")
}

func (f *installationStateFlag) Set(s string) error {
	if *f == nil {
		*f = make(installationStateFlag)
	}
	states := strings.Split(s, ",")
	for _, s := range states {
		state, err := device.InstallationStateFromString(s)
		if err != nil {
			return err
		}
		(*f)[state] = struct{}{}
	}
	return nil
}

var (
	instanceStateFilter     instanceStateFlag
	installationStateFilter installationStateFlag
	onlyInstances           bool
	onlyInstallations       bool
)

func init() {
	// NOTE: When addind new flags or changing default values, remember to
	// also update ResetGlobFlags below.
	flag.Var(&instanceStateFilter, "instance-state", fmt.Sprintf("If non-empty, specifies allowed instance states (all other instances get filtered out). The value of the flag is a comma-separated list of values from among: %v.", device.InstanceStateAll))
	flag.Var(&installationStateFilter, "installation-state", fmt.Sprintf("If non-empty, specifies allowed installation states (all others installations get filtered out). The value of the flag is a comma-separated list of values from among: %v.", device.InstallationStateAll))
	flag.BoolVar(&onlyInstances, "only-instances", false, "If set, only consider instances.")
	flag.BoolVar(&onlyInstallations, "only-installations", false, "If set, only consider installations.")
}

// ResetGlobFlags is meant for tests to restore the values of flag-configured
// variables when running multiple commands in the same process.
func ResetGlobFlags() {
	instanceStateFilter = make(instanceStateFlag)
	installationStateFilter = make(installationStateFlag)
	onlyInstances = false
	onlyInstallations = false
}
