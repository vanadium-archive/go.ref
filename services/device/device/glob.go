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
	"sync/atomic"
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

// GlobHandler is implemented by each command that wants to execute against name
// patterns.  The handler is expected to be invoked against each glob result,
// and can be run concurrently. The handler should direct its output to the
// given stdout and stderr writers.
//
// There are three usage patterns, depending on the desired level of control
// over the execution flow and settings manipulation.
//
// (1) Most control
//
// func myCmdHandler(entry GlobResult, ctx *context.T, stdout, stderr io.Writer) error {
//   output := myCmdProcessing(entry)
//   fmt.Fprintf(stdout, output)
//   ...
// }
//
// func runMyCmd(ctx *context.T, env *cmdline.Env, args []string) error {
//   ...
//   err := Run(ctx, env, args, myCmdHandler, GlobSettings{})
//   ...
// }
//
// var cmdMyCmd = &cmdline.Command {
//   Runner: v23cmd.RunnerFunc(runMyCmd)
//   ...
// }
//
// (2) Pre-packaged runner
//
// If all runMyCmd does is to call Run, you can use globRunner to avoid having
// to define runMyCmd:
//
// var cmdMyCmd = &cmdline.Command {
//   Runner: globRunner(myCmdHandler, &GlobSettings{}),
//   Name: "mycmd",
//   ...
// }
//
// (3) Pre-packaged runner and glob settings flag configuration
//
// If, additionally, you want the GlobSettings to be configurable with
// command-line flags, you can use globify instead:
//
// var cmdMyCmd = &cmdline.Command {
//   Name: "mycmd",
//   ...
// }
//
// func init() {
//   globify(cmdMyCmd, myCmdHandler, &GlobSettings{}),
// }
//
// The GlobHandler identifier is exported for use in unit tests.
type GlobHandler func(entry GlobResult, ctx *context.T, stdout, stderr io.Writer) error

func globRunner(handler GlobHandler, s *GlobSettings) cmdline.Runner {
	return v23cmd.RunnerFunc(func(ctx *context.T, env *cmdline.Env, args []string) error {
		return Run(ctx, env, args, handler, *s)
	})
}

type objectKind int

const (
	ApplicationInstallationObject objectKind = iota
	ApplicationInstanceObject
	DeviceServiceObject
	sentinelObjectKind
)

var objectKinds = []objectKind{
	ApplicationInstallationObject,
	ApplicationInstanceObject,
	DeviceServiceObject,
}

func init() {
	// TODO(caprita): Move to glob_test.go once that exists.
	if len(objectKinds) != int(sentinelObjectKind) {
		panic(fmt.Sprintf("broken invariant: mismatching number of object kinds"))
	}
}

// GlobResult defines the input to a GlobHandler.
// The identifier is exported for use in unit tests.
type GlobResult struct {
	Name   string
	Status device.Status
	Kind   objectKind
}

func NewGlobResult(name string, status device.Status) (*GlobResult, error) {
	var kind objectKind
	switch status.(type) {
	case device.StatusInstallation:
		kind = ApplicationInstallationObject
	case device.StatusInstance:
		kind = ApplicationInstanceObject
	case device.StatusDevice:
		kind = DeviceServiceObject
	default:
		return nil, fmt.Errorf("Status(%v) returned unrecognized status type %T\n", name, status)
	}
	return &GlobResult{
		Name:   name,
		Status: status,
		Kind:   kind,
	}, nil
}

type byTypeAndName []*GlobResult

func (a byTypeAndName) Len() int      { return len(a) }
func (a byTypeAndName) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

func (a byTypeAndName) Less(i, j int) bool {
	r1, r2 := a[i], a[j]
	if r1.Kind != r2.Kind {
		return r1.Kind < r2.Kind
	}
	return r1.Name < r2.Name
}

// Run runs the given handler in parallel against each of the results obtained
// by globbing args, after performing filtering based on type
// (instance/installation) and state.  No de-duping of results is performed.
// The outputs from each of the handlers are sorted: installations first, then
// instances (and alphabetically by object name for each group).
// The identifier is exported for use in unit tests.
func Run(ctx *context.T, env *cmdline.Env, args []string, handler GlobHandler, s GlobSettings) error {
	results := glob(ctx, env, args)
	sort.Sort(byTypeAndName(results))
	results = filterResults(results, s)
	stdouts, stderrs := make([]bytes.Buffer, len(results)), make([]bytes.Buffer, len(results))
	var errorCounter uint32 = 0
	perResult := func(r *GlobResult, index int) {
		if err := handler(*r, ctx, &stdouts[index], &stderrs[index]); err != nil {
			fmt.Fprintf(&stderrs[index], "ERROR for \"%s\": %v.\n", r.Name, err)
			atomic.AddUint32(&errorCounter, 1)
		}
	}
	// TODO(caprita): Add unit test logic to cover all parallelism options.
	switch s.HandlerParallelism {
	case FullParallelism:
		var wg sync.WaitGroup
		for i, r := range results {
			wg.Add(1)
			go func(r *GlobResult, i int) {
				perResult(r, i)
				wg.Done()
			}(r, i)
		}
		wg.Wait()
	case NoParallelism:
		for i, r := range results {
			perResult(r, i)
		}
	case KindParallelism:
		processed := 0
		for _, k := range objectKinds {
			var wg sync.WaitGroup
			for i, r := range results {
				if r.Kind != k {
					continue
				}
				wg.Add(1)
				processed++
				go func(r *GlobResult, i int) {
					perResult(r, i)
					wg.Done()
				}(r, i)
			}
			wg.Wait()
		}
		if processed != len(results) {
			return fmt.Errorf("broken invariant: unhandled object kind")
		}
	}
	for i := range results {
		io.Copy(env.Stdout, &stdouts[i])
		io.Copy(env.Stderr, &stderrs[i])
	}
	if errorCounter > 0 {
		return fmt.Errorf("encountered a total of %d error(s)", errorCounter)
	}
	return nil
}

func filterResults(results []*GlobResult, s GlobSettings) []*GlobResult {
	var ret []*GlobResult
	for _, r := range results {
		switch status := r.Status.(type) {
		case device.StatusInstance:
			if s.onlyInstallations || !s.instanceStateFilter.apply(status.Value.State) {
				continue
			}
		case device.StatusInstallation:
			if s.onlyInstances || !s.installationStateFilter.apply(status.Value.State) {
				continue
			}
		}
		ret = append(ret, r)
	}
	return ret
}

// TODO(caprita): We need to filter out debug objects under the app instances'
// namespaces, so that the tool works with ... patterns.  We should change glob
// on device manager to not return debug objects by default for apps and instead
// put them under a __debug suffix (like it works for services).
var debugNameRE = regexp.MustCompile("/apps/[^/]+/[^/]+/[^/]+/(logs|stats|pprof)(/|$)")

func getStatus(ctx *context.T, env *cmdline.Env, name string, resultsCh chan<- *GlobResult) {
	status, err := device.DeviceClient(name).Status(ctx)
	// Skip non-instances/installations.
	if verror.ErrorID(err) == deviceimpl.ErrInvalidSuffix.ID {
		return
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "Status(%v) failed: %v\n", name, err)
		return
	}
	if r, err := NewGlobResult(name, status); err != nil {
		fmt.Fprintf(env.Stderr, "%v\n", err)
	} else {
		resultsCh <- r
	}
}

func globOne(ctx *context.T, env *cmdline.Env, pattern string, resultsCh chan<- *GlobResult) {
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
func glob(ctx *context.T, env *cmdline.Env, args []string) []*GlobResult {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var wg sync.WaitGroup
	resultsCh := make(chan *GlobResult, 100)
	// For each pattern.
	for _, a := range args {
		wg.Add(1)
		go func(pattern string) {
			globOne(ctx, env, pattern, resultsCh)
			wg.Done()
		}(a)
	}
	// Collect the glob results into a slice.
	var results []*GlobResult
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

type parallelismFlag int

const (
	FullParallelism parallelismFlag = iota
	NoParallelism
	KindParallelism
	sentinelParallelismFlag
)

var parallelismStrings = map[parallelismFlag]string{
	FullParallelism: "FULL",
	NoParallelism:   "NONE",
	KindParallelism: "BYKIND",
}

func init() {
	if len(parallelismStrings) != int(sentinelParallelismFlag) {
		panic(fmt.Sprintf("broken invariant: mismatching number of parallelism types"))
	}
}

func (p *parallelismFlag) String() string {
	s, ok := parallelismStrings[*p]
	if !ok {
		return "UNKNOWN"
	}
	return s
}

func (p *parallelismFlag) Set(s string) error {
	for k, v := range parallelismStrings {
		if s == v {
			*p = k
			return nil
		}
	}
	return fmt.Errorf("unrecognized parallelism type: %v", s)
}

// GlobSettings specifies flag-settable options and filters for globbing.
// The identifier is exported for use in unit tests.
type GlobSettings struct {
	instanceStateFilter     instanceStateFlag
	installationStateFilter installationStateFlag
	onlyInstances           bool
	onlyInstallations       bool
	HandlerParallelism      parallelismFlag
	defaults                *GlobSettings
}

func (s *GlobSettings) reset() {
	d := s.defaults
	*s = *d
	s.defaults = d
}

func (s *GlobSettings) setDefaults(d GlobSettings) {
	s.defaults = new(GlobSettings)
	*s.defaults = d
}

var allGlobSettings []*GlobSettings

// ResetGlobSettings is meant for tests to restore the values of flag-configured
// variables when running multiple commands in the same process.
func ResetGlobSettings() {
	for _, s := range allGlobSettings {
		s.reset()
	}
}

func defineGlobFlags(fs *flag.FlagSet, s *GlobSettings) {
	fs.Var(&s.instanceStateFilter, "instance-state", fmt.Sprintf("If non-empty, specifies allowed instance states (all other instances get filtered out). The value of the flag is a comma-separated list of values from among: %v.", device.InstanceStateAll))
	fs.Var(&s.installationStateFilter, "installation-state", fmt.Sprintf("If non-empty, specifies allowed installation states (all others installations get filtered out). The value of the flag is a comma-separated list of values from among: %v.", device.InstallationStateAll))
	fs.BoolVar(&s.onlyInstances, "only-instances", false, "If set, only consider instances.")
	fs.BoolVar(&s.onlyInstallations, "only-installations", false, "If set, only consider installations.")
	var parallelismValues []string
	for _, v := range parallelismStrings {
		parallelismValues = append(parallelismValues, v)
	}
	sort.Strings(parallelismValues)
	fs.Var(&s.HandlerParallelism, "parallelism", fmt.Sprintf("Specifies the level of parallelism for the handler execution. One of: %v.", parallelismValues))
}

func globify(c *cmdline.Command, handler GlobHandler, s *GlobSettings) {
	s.setDefaults(*s)
	defineGlobFlags(&c.Flags, s)
	c.Runner = globRunner(handler, s)
	allGlobSettings = append(allGlobSettings, s)
}
