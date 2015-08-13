// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sysstats implements system statistics and updates them periodically.
//
// Importing this package causes the stats to be exported via an init function.
package sysstats

import (
	"os"
	"reflect"
	"runtime"
	"strings"
	"time"

	"v.io/x/lib/metadata"
	"v.io/x/ref/lib/stats"
)

func init() {
	now := time.Now()
	stats.NewInteger("system/start-time-unix").Set(now.Unix())
	stats.NewString("system/start-time-rfc1123").Set(now.Format(time.RFC1123))
	stats.NewString("system/cmdline").Set(strings.Join(os.Args, " "))
	stats.NewInteger("system/num-cpu").Set(int64(runtime.NumCPU()))
	stats.NewIntegerFunc("system/num-goroutine", func() int64 { return int64(runtime.NumGoroutine()) })
	stats.NewString("system/version").Set(runtime.Version())
	stats.NewInteger("system/pid").Set(int64(os.Getpid()))
	if hostname, err := os.Hostname(); err == nil {
		stats.NewString("system/hostname").Set(hostname)
	}
	stats.NewIntegerFunc("system/GOMAXPROCS", func() int64 { return int64(runtime.GOMAXPROCS(0)) })
	exportEnv()
	exportMemStats()
	exportMetaData()
}

func exportEnv() {
	var kv []stats.KeyValue
	for _, v := range os.Environ() {
		if parts := strings.SplitN(v, "=", 2); len(parts) == 2 {
			kv = append(kv, stats.KeyValue{
				Key:   parts[0],
				Value: parts[1],
			})
		}
	}
	stats.NewMap("system/environ").Set(kv)
}

func exportMemStats() {
	mstats := stats.NewMap("system/memstats")

	// Get field names to export.
	var memstats runtime.MemStats
	fieldNames := []string{}
	v := reflect.ValueOf(memstats)
	v.FieldByNameFunc(func(name string) bool {
		switch v.FieldByName(name).Kind() {
		case reflect.Bool, reflect.Uint32, reflect.Uint64:
			fieldNames = append(fieldNames, name)
		}
		return false
	})
	updateStats := func() {
		var memstats runtime.MemStats
		runtime.ReadMemStats(&memstats)
		v := reflect.ValueOf(memstats)
		kv := make([]stats.KeyValue, len(fieldNames))
		for i, name := range fieldNames {
			kv[i] = stats.KeyValue{
				Key:   name,
				Value: v.FieldByName(name).Interface(),
			}
		}
		mstats.Set(kv)
	}
	// Update stats now and every 10 seconds afterwards.
	updateStats()
	go func() {
		for {
			time.Sleep(10 * time.Second)
			updateStats()
		}
	}()
}

func exportMetaData() {
	var kv []stats.KeyValue
	for id, value := range metadata.ToMap() {
		kv = append(kv, stats.KeyValue{
			Key:   id,
			Value: value,
		})
	}
	stats.NewMap("system/metadata").Set(kv)
}
