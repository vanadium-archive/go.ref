// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Note: We disable durEq and pingResultsEq when the race detector is enabled;
// they rely on timings that are thrown off by the race detector, which may
// increase execution time by 2-20x. We also disable these checks when running
// in Jenkins, because Jenkins machines are sometimes exceptionally slow.

package ping_test

import (
	"errors"
	"os"
	"reflect"
	"runtime/debug"
	"strconv"
	"testing"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/syncbase/ping"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/test"
)

////////////////////////////////////////////////////////////////////////////////
// Generic helpers

func fatal(t *testing.T, args ...interface{}) {
	debug.PrintStack()
	t.Fatal(args...)
}

func fatalf(t *testing.T, format string, args ...interface{}) {
	debug.PrintStack()
	t.Fatalf(format, args...)
}

func eq(t *testing.T, got, want interface{}) {
	if !reflect.DeepEqual(got, want) {
		fatalf(t, "got %v, want %v", got, want)
	}
}

// TODO(jingjin): Detects whether this is Jenkins. Modeled after isCI function
// in v.io/x/devtools/jiri-test/internal/test/predicates.go. We should put this
// in some public location.
var isCI = os.Getenv("USER") == "veyron" || os.Getenv("V23_FORCE_CI") == "yes"

////////////////////////////////////////////////////////////////////////////////
// TestPingInParallel

type pingable struct {
	delay time.Duration // how long to wait before replying to a ping
}

func (p *pingable) Ping(_ *context.T, _ rpc.ServerCall) error {
	time.Sleep(p.delay)
	return nil
}

// startPingableServer starts a server that can handle Ping RPCs. It returns the
// server's name (endpoint) and a cleanup func.
func startPingableServer(t *testing.T, ctx *context.T, delay time.Duration) (string, func()) {
	service := interfaces.PingableServer(&pingable{delay: delay})
	ctx, server, err := v23.WithNewServer(ctx, "", service, nil)
	if err != nil {
		fatal(t, err)
	}
	return server.Status().Endpoints[0].Name(), func() { server.Stop() }
}

func ms(d time.Duration) time.Duration {
	return d * time.Millisecond
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		d = -d
	}
	return d
}

// durEq checks that the given durations are equal, with a bit of slack.
func durEq(t *testing.T, got, want time.Duration) {
	if isCI || raceDetectorEnabled {
		return // See comment at the top of this file.
	}
	// TODO(sadovsky): Reduce the slack once Vanadium implements a fast path for
	// local RPCs.
	if abs(got-want) > ms(15) { // 15ms slack
		eq(t, got, want)
	}
}

// pingResultsEq checks that the given ping results are equal, with a bit of
// slack around expected durations. Errors are considered equal if their verror
// IDs are equal.
func pingResultsEq(t *testing.T, got, want map[string]ping.PingResult) {
	if isCI || raceDetectorEnabled {
		return // See comment at the top of this file.
	}
	if len(got) != len(want) {
		eq(t, got, want)
	}
	for name, gotRes := range got {
		wantRes, ok := want[name]
		if !ok {
			eq(t, got, want)
		}
		eq(t, gotRes.Name, wantRes.Name)
		eq(t, verror.ErrorID(gotRes.Err), verror.ErrorID(wantRes.Err))
		durEq(t, gotRes.Dur, wantRes.Dur)
	}
}

var (
	errCanceled = verror.NewErrCanceled(nil)
	errTimeout  = verror.NewErrTimeout(nil)
)

func TestPingInParallel(t *testing.T) {
	ctx, shutdown := test.V23Init()
	defer shutdown()

	var cleanups []func()
	defer func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()

	serve := func(delay time.Duration) string {
		name, cleanup := startPingableServer(t, ctx, delay)
		cleanups = append(cleanups, cleanup)
		return name
	}

	// Start some pingable servers, each with its own delay.
	a, b, c := serve(ms(10)), serve(ms(50)), serve(ms(90))

	// NOTE(sadovsky): The first RPC to a given server carries a ~20ms overhead
	// (ouch!); subsequent RPCs have ~5ms overhead (ouch, but not as much ouch).
	// We do one round of PingInParallel to warm up all the servers so that we can
	// use tighter bounds on expected durations in pingResultsEq.
	res, err := ping.PingInParallel(ctx, []string{a, b, c}, ms(200), ms(200))
	eq(t, err, nil)

	start := time.Now()
	res, err = ping.PingInParallel(ctx, []string{a, b, c}, ms(200), ms(200))
	eq(t, err, nil)
	// The slowest server has a 90ms delay, so PingInParallel should complete
	// after roughly 90ms, i.e. well before the 200ms timeout.
	durEq(t, time.Now().Sub(start), ms(90))
	pingResultsEq(t, res, map[string]ping.PingResult{
		a: {Name: a, Dur: ms(10)},
		b: {Name: b, Dur: ms(50)},
		c: {Name: c, Dur: ms(90)},
	})

	// No slack.
	start = time.Now()
	res, err = ping.PingInParallel(ctx, []string{a, b, c}, 0, ms(200))
	eq(t, err, nil)
	durEq(t, time.Now().Sub(start), ms(10))
	pingResultsEq(t, res, map[string]ping.PingResult{
		a: {Name: a, Dur: ms(10)},
		b: {Name: b, Dur: ms(10), Err: errCanceled},
		c: {Name: c, Dur: ms(10), Err: errCanceled},
	})

	// 60ms slack: enough for b, but not for c.
	start = time.Now()
	res, err = ping.PingInParallel(ctx, []string{a, b, c}, ms(60), ms(200))
	eq(t, err, nil)
	durEq(t, time.Now().Sub(start), ms(10+60))
	pingResultsEq(t, res, map[string]ping.PingResult{
		a: {Name: a, Dur: ms(10)},
		b: {Name: b, Dur: ms(50)},
		c: {Name: c, Dur: ms(10 + 60), Err: errCanceled},
	})

	// Same as above, but with names in a different order to demonstrate that the
	// order doesn't matter.
	start = time.Now()
	res, err = ping.PingInParallel(ctx, []string{c, b, a}, ms(60), ms(200))
	eq(t, err, nil)
	durEq(t, time.Now().Sub(start), ms(10+60))
	pingResultsEq(t, res, map[string]ping.PingResult{
		a: {Name: a, Dur: ms(10)},
		b: {Name: b, Dur: ms(50)},
		c: {Name: c, Dur: ms(10 + 60), Err: errCanceled},
	})

	// 60ms slack, 30ms timeout: slack is enough for b, but timeout trumps slack.
	start = time.Now()
	res, err = ping.PingInParallel(ctx, []string{a, b, c}, ms(60), ms(30))
	eq(t, err, nil)
	durEq(t, time.Now().Sub(start), ms(30))
	pingResultsEq(t, res, map[string]ping.PingResult{
		a: {Name: a, Dur: ms(10)},
		b: {Name: b, Dur: ms(30), Err: errTimeout},
		c: {Name: c, Dur: ms(30), Err: errTimeout},
	})

	// 200ms slack, 0ms timeout: no server responds in time.
	start = time.Now()
	res, err = ping.PingInParallel(ctx, []string{a, b, c}, ms(200), ms(0))
	eq(t, err, nil)
	durEq(t, time.Now().Sub(start), ms(0))
	pingResultsEq(t, res, map[string]ping.PingResult{
		a: {Name: a, Dur: ms(0), Err: errTimeout},
		b: {Name: b, Dur: ms(0), Err: errTimeout},
		c: {Name: c, Dur: ms(0), Err: errTimeout},
	})

	// 200ms slack, 70ms timeout: slack is enough for both b and c, but timeout
	// causes c to fail.
	start = time.Now()
	res, err = ping.PingInParallel(ctx, []string{a, b, c}, ms(200), ms(70))
	eq(t, err, nil)
	durEq(t, time.Now().Sub(start), ms(70))
	pingResultsEq(t, res, map[string]ping.PingResult{
		a: {Name: a, Dur: ms(10)},
		b: {Name: b, Dur: ms(50)},
		c: {Name: c, Dur: ms(70), Err: errTimeout},
	})

	// No servers to ping.
	start = time.Now()
	res, err = ping.PingInParallel(ctx, []string{}, ms(20), ms(0))
	eq(t, err, nil)
	durEq(t, time.Now().Sub(start), ms(0))
	pingResultsEq(t, res, map[string]ping.PingResult{})
}

////////////////////////////////////////////////////////////////////////////////
// TestSortByDur

var errPingFailed = errors.New("ping failed")

func TestSortByDur(t *testing.T) {
	var m map[string]ping.PingResult

	// Empty map.
	eq(t, ping.SortByDur(m), []ping.PingResult{})

	// Map with one failed ping.
	m = map[string]ping.PingResult{
		"a": {Name: "a", Err: errPingFailed},
	}
	eq(t, ping.SortByDur(m), []ping.PingResult{})

	// Map with two failed pings.
	m = map[string]ping.PingResult{
		"a": {Name: "a", Err: errPingFailed},
		"b": {Name: "b", Err: errPingFailed},
	}
	eq(t, ping.SortByDur(m), []ping.PingResult{})

	// Map with one OK ping.
	aRes := ping.PingResult{Name: "a", Dur: 5}
	m = map[string]ping.PingResult{
		"a": aRes,
	}
	eq(t, ping.SortByDur(m), []ping.PingResult{aRes})

	// Map with two OK pings.
	bRes := ping.PingResult{Name: "b", Dur: 4}
	m = map[string]ping.PingResult{
		"a": aRes,
		"b": bRes,
	}
	eq(t, ping.SortByDur(m), []ping.PingResult{bRes, aRes}) // b.Dur < a.Dur

	// Map with mix of OK and failed pings.
	m = map[string]ping.PingResult{
		"a": aRes,
		"b": bRes,
		"c": {Name: "c", Err: errPingFailed},
	}
	eq(t, ping.SortByDur(m), []ping.PingResult{bRes, aRes}) // b.Dur < a.Dur

	// Map with lots of OK and failed pings.
	m = map[string]ping.PingResult{}
	for i := 0; i < 10; i++ {
		name := strconv.Itoa(i)
		err := error(nil)
		if i%2 == 0 {
			err = errPingFailed
		}
		m[name] = ping.PingResult{Name: name, Err: err, Dur: time.Duration(i % 3)}
	}
	eq(t, ping.SortByDur(m), []ping.PingResult{
		{Name: "3", Dur: 0},
		{Name: "9", Dur: 0},
		{Name: "1", Dur: 1},
		{Name: "7", Dur: 1},
		{Name: "5", Dur: 2},
	})
}
