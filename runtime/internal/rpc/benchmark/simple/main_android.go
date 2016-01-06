// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build android

// Android "app" to run the RPC benchmarks.
//
// Usage: See run-android.sh
package main

import (
	"time"

	"golang.org/x/mobile/app"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/gl"
)

func main() {
	done := make(chan struct{})
	go func() {
		realMain()
		close(done)
	}()
	app.Main(func(a app.App) {
		var glctx gl.Context
		ticks := time.Tick(time.Second / 2)
		black := false
		for {
			select {
			case <-done:
				done = nil
				a.Send(paint.Event{})
			case <-ticks:
				black = !black
				a.Send(paint.Event{})
			case e := <-a.Events():
				switch e := a.Filter(e).(type) {
				case lifecycle.Event:
					glctx, _ = e.DrawContext.(gl.Context)
				case paint.Event:
					if glctx == nil {
						continue
					}
					// solid green        success
					// flashing red/blue: working
					if done == nil {
						glctx.ClearColor(0, 1, 0, 1)
					} else if black {
						glctx.ClearColor(0, 0, 0, 1)
					} else {
						glctx.ClearColor(0, 0, 1, 1)
					}
					glctx.Clear(gl.COLOR_BUFFER_BIT)
					a.Publish()
				}
			}
		}
	})
}
