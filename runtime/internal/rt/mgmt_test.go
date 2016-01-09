// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rt_test

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/services/appcycle"
	"v.io/x/ref/lib/mgmt"
	"v.io/x/ref/lib/security/securityflag"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/device"
	"v.io/x/ref/test"
	"v.io/x/ref/test/expect"
	"v.io/x/ref/test/modules"
)

// TestBasic verifies that the basic plumbing works: LocalStop calls result in
// stop messages being sent on the channel passed to WaitForStop.
func TestBasic(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	m := v23.GetAppCycle(ctx)
	ch := make(chan string, 1)
	m.WaitForStop(ctx, ch)
	for i := 0; i < 10; i++ {
		m.Stop(ctx)
		if want, got := v23.LocalStop, <-ch; want != got {
			t.Errorf("WaitForStop want %q got %q", want, got)
		}
		select {
		case s := <-ch:
			t.Errorf("channel expected to be empty, got %q instead", s)
		default:
		}
	}
}

// TestMultipleWaiters verifies that the plumbing works with more than one
// registered wait channel.
func TestMultipleWaiters(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	m := v23.GetAppCycle(ctx)
	ch1 := make(chan string, 1)
	m.WaitForStop(ctx, ch1)
	ch2 := make(chan string, 1)
	m.WaitForStop(ctx, ch2)
	for i := 0; i < 10; i++ {
		m.Stop(ctx)
		if want, got := v23.LocalStop, <-ch1; want != got {
			t.Errorf("WaitForStop want %q got %q", want, got)
		}
		if want, got := v23.LocalStop, <-ch2; want != got {
			t.Errorf("WaitForStop want %q got %q", want, got)
		}
	}
}

// TestMultipleStops verifies that LocalStop does not block even if the wait
// channel is not being drained: once the channel's buffer fills up, future
// Stops become no-ops.
func TestMultipleStops(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	m := v23.GetAppCycle(ctx)
	ch := make(chan string, 1)
	m.WaitForStop(ctx, ch)
	for i := 0; i < 10; i++ {
		m.Stop(ctx)
	}
	if want, got := v23.LocalStop, <-ch; want != got {
		t.Errorf("WaitForStop want %q got %q", want, got)
	}
	select {
	case s := <-ch:
		t.Errorf("channel expected to be empty, got %q instead", s)
	default:
	}
}

var noWaiters = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	m := v23.GetAppCycle(ctx)
	fmt.Fprintf(env.Stdout, "ready\n")
	modules.WaitForEOF(env.Stdin)
	m.Stop(ctx)
	os.Exit(42) // This should not be reached.
	return nil
}, "noWaiters")

// TestNoWaiters verifies that the child process exits in the absence of any
// wait channel being registered with its runtime.
func TestNoWaiters(t *testing.T) {
	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start(nil, noWaiters)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	h.Expect("ready")
	want := fmt.Sprintf("exit status %d", testUnhandledStopExitCode)
	if err = h.Shutdown(os.Stderr, os.Stderr); err == nil || err.Error() != want {
		t.Errorf("got %v, want %s", err, want)
	}
}

var forceStop = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	m := v23.GetAppCycle(ctx)
	fmt.Fprintf(env.Stdout, "ready\n")
	modules.WaitForEOF(env.Stdin)
	m.WaitForStop(ctx, make(chan string, 1))
	m.ForceStop(ctx)
	os.Exit(42) // This should not be reached.
	return nil
}, "forceStop")

// TestForceStop verifies that ForceStop causes the child process to exit
// immediately.
func TestForceStop(t *testing.T) {
	sh, err := modules.NewShell(nil, nil, testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer sh.Cleanup(os.Stderr, os.Stderr)
	h, err := sh.Start(nil, forceStop)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	h.Expect("ready")
	err = h.Shutdown(os.Stderr, os.Stderr)
	want := fmt.Sprintf("exit status %d", testForceStopExitCode)
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %s", err, want)
	}
}

func checkProgress(t *testing.T, ch <-chan v23.Task, progress, goal int32) {
	if want, got := (v23.Task{Progress: progress, Goal: goal}), <-ch; !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected progress: want %+v, got %+v", want, got)
	}
}

func checkNoProgress(t *testing.T, ch <-chan v23.Task) {
	select {
	case p := <-ch:
		t.Errorf("channel expected to be empty, got %+v instead", p)
	default:
	}
}

// TestProgress verifies that the ticker update/track logic works for a single
// tracker.
func TestProgress(t *testing.T) {
	ctx, shutdown := test.V23Init()

	m := v23.GetAppCycle(ctx)
	m.AdvanceGoal(50)
	ch := make(chan v23.Task, 1)
	m.TrackTask(ch)
	checkNoProgress(t, ch)
	m.AdvanceProgress(10)
	checkProgress(t, ch, 10, 50)
	checkNoProgress(t, ch)
	m.AdvanceProgress(5)
	checkProgress(t, ch, 15, 50)
	m.AdvanceGoal(50)
	checkProgress(t, ch, 15, 100)
	m.AdvanceProgress(1)
	checkProgress(t, ch, 16, 100)
	m.AdvanceGoal(10)
	checkProgress(t, ch, 16, 110)
	m.AdvanceProgress(-13)
	checkNoProgress(t, ch)
	m.AdvanceGoal(0)
	checkNoProgress(t, ch)
	shutdown()
	if _, ok := <-ch; ok {
		t.Errorf("Expected channel to be closed")
	}
}

// TestProgressMultipleTrackers verifies that the ticker update/track logic
// works for more than one tracker.  It also ensures that the runtime doesn't
// block when the tracker channels are full.
func TestProgressMultipleTrackers(t *testing.T) {
	ctx, shutdown := test.V23Init()

	m := v23.GetAppCycle(ctx)
	// ch1 is 1-buffered, ch2 is 2-buffered.
	ch1, ch2 := make(chan v23.Task, 1), make(chan v23.Task, 2)
	m.TrackTask(ch1)
	m.TrackTask(ch2)
	checkNoProgress(t, ch1)
	checkNoProgress(t, ch2)
	m.AdvanceProgress(1)
	checkProgress(t, ch1, 1, 0)
	checkNoProgress(t, ch1)
	checkProgress(t, ch2, 1, 0)
	checkNoProgress(t, ch2)
	for i := 0; i < 10; i++ {
		m.AdvanceProgress(1)
	}
	checkProgress(t, ch1, 2, 0)
	checkNoProgress(t, ch1)
	checkProgress(t, ch2, 2, 0)
	checkProgress(t, ch2, 3, 0)
	checkNoProgress(t, ch2)
	m.AdvanceGoal(4)
	checkProgress(t, ch1, 11, 4)
	checkProgress(t, ch2, 11, 4)
	shutdown()
	if _, ok := <-ch1; ok {
		t.Errorf("Expected channel to be closed")
	}
	if _, ok := <-ch2; ok {
		t.Errorf("Expected channel to be closed")
	}
}

var app = modules.Register(func(env *modules.Env, args ...string) error {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	m := v23.GetAppCycle(ctx)
	ch := make(chan string, 1)
	m.WaitForStop(ctx, ch)
	fmt.Fprintln(env.Stdout, "ready")
	fmt.Fprintf(env.Stdout, "Got %s\n", <-ch)
	m.AdvanceGoal(10)
	fmt.Fprintf(env.Stdout, "Doing some work\n")
	m.AdvanceProgress(2)
	fmt.Fprintf(env.Stdout, "Doing some more work\n")
	m.AdvanceProgress(5)
	return nil
}, "app")

type configServer struct {
	ch chan<- string
}

func (c *configServer) Set(_ *context.T, _ rpc.ServerCall, key, value string) error {
	if key != mgmt.AppCycleManagerConfigKey {
		return fmt.Errorf("Unexpected key: %v", key)
	}
	c.ch <- value
	return nil

}

// TODO(caprita): Rewrite this test to not use the modules package. It can't use
// v23test because v23test.Shell doesn't support the exec protocol.
func setupRemoteAppCycleMgr(t *testing.T) (*context.T, modules.Handle, appcycle.AppCycleClientMethods, func()) {
	ctx, shutdown := test.V23Init()

	ch := make(chan string)
	service := device.ConfigServer(&configServer{ch})
	authorizer := securityflag.NewAuthorizerOrDie()
	_, configServer, err := v23.WithNewServer(ctx, "", service, authorizer)
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	configServiceName := configServer.Status().Endpoints[0].Name()

	sh, err := modules.NewShell(ctx, v23.GetPrincipal(ctx), testing.Verbose(), t)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	sh.SetConfigKey(mgmt.ParentNameConfigKey, configServiceName)
	sh.SetConfigKey(mgmt.ProtocolConfigKey, "tcp")
	sh.SetConfigKey(mgmt.AddressConfigKey, "127.0.0.1:0")
	h, err := sh.Start(nil, app)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	appCycleName := ""
	select {
	case appCycleName = <-ch:
	case <-time.After(time.Minute):
		t.Errorf("timeout")
	}
	appCycle := appcycle.AppCycleClient(appCycleName)
	return ctx, h, appCycle, func() {
		configServer.Stop()
		sh.Cleanup(os.Stderr, os.Stderr)
		shutdown()
	}
}

// TestRemoteForceStop verifies that the child process exits when sending it
// a remote ForceStop rpc.
func TestRemoteForceStop(t *testing.T) {
	ctx, h, appCycle, cleanup := setupRemoteAppCycleMgr(t)
	defer cleanup()
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("ready")
	if err := appCycle.ForceStop(ctx); err == nil || !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("Expected EOF error, got %v instead", err)
	}
	s.ExpectEOF()
	err := h.Shutdown(os.Stderr, os.Stderr)
	want := fmt.Sprintf("exit status %d", testForceStopExitCode)
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %s", err, want)
	}
}

// TestRemoteStop verifies that the child shuts down cleanly when sending it
// a remote Stop rpc.
func TestRemoteStop(t *testing.T) {
	ctx, h, appCycle, cleanup := setupRemoteAppCycleMgr(t)
	defer cleanup()
	s := expect.NewSession(t, h.Stdout(), time.Minute)
	s.Expect("ready")
	stream, err := appCycle.Stop(ctx)
	if err != nil {
		t.Fatalf("Got error: %v", err)
	}
	rStream := stream.RecvStream()
	expectTask := func(progress, goal int32) {
		if !rStream.Advance() {
			t.Fatalf("unexpected streaming error: %v", rStream.Err())
		}
		task := rStream.Value()
		if task.Progress != progress || task.Goal != goal {
			t.Errorf("Got (%d, %d), want (%d, %d)", task.Progress, task.Goal, progress, goal)
		}
	}
	expectTask(0, 10)
	expectTask(2, 10)
	expectTask(7, 10)
	if rStream.Advance() || rStream.Err() != nil {
		t.Errorf("Expected EOF, got (%v, %v) instead", rStream.Value(), rStream.Err())
	}
	if err := stream.Finish(); err != nil {
		t.Errorf("Got error %v", err)
	}
	s.Expect(fmt.Sprintf("Got %s", v23.RemoteStop))
	s.Expect("Doing some work")
	s.Expect("Doing some more work")
	s.ExpectEOF()
	if err := h.Shutdown(os.Stderr, os.Stderr); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}
