// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sysstats implements system statistics and updates them periodically.
//
// Importing this package causes the stats to be exported via an init function.
package sysstats

import (
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/mem"

	"v.io/v23/naming"
	"v.io/x/lib/metadata"
	"v.io/x/ref"
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
	exportSysMem()
	exportSysCpu()
	exportSysDisk()
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
	fieldNames := getFieldNames(memstats)
	updateStats := func() {
		var memstats runtime.MemStats
		runtime.ReadMemStats(&memstats)
		updateStatsMap(mstats, memstats, fieldNames)
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

func exportSysMem() {
	sysMemStats := stats.NewMap("system/sysmem")

	// Get field names to export.
	var s mem.VirtualMemoryStat
	fieldNames := getFieldNames(s)

	// Update stats now and every 10 seconds afterwards.
	updateStats := func() {
		if s, err := mem.VirtualMemory(); err == nil {
			updateStatsMap(sysMemStats, *s, fieldNames)
		}
	}
	go func() {
		for {
			updateStats()
			time.Sleep(10 * time.Second)
		}
	}()
}

func exportSysCpu() {
	sysCpuStats := stats.NewMap("system/syscpu")

	// Get field names to export.
	type cpuStat struct {
		Percent   float64
		User      float64
		System    float64
		Idle      float64
		Nice      float64
		Iowait    float64
		Irq       float64
		Softirq   float64
		Steal     float64
		Guest     float64
		GuestNice float64
		Stolen    float64
	}
	var s cpuStat
	fieldNames := getFieldNames(s)

	// Update stats now and every 10 seconds afterwards.
	updateStats := func() {
		pcts, err := cpu.Percent(time.Second*3, false)
		if err != nil || len(pcts) == 0 {
			return
		}
		times, err := cpu.Times(false)
		if err != nil || len(times) == 0 {
			return
		}
		t := times[0]
		s := cpuStat{
			Percent:   pcts[0],
			User:      t.User,
			System:    t.System,
			Idle:      t.Idle,
			Nice:      t.Nice,
			Iowait:    t.Iowait,
			Irq:       t.Irq,
			Softirq:   t.Softirq,
			Steal:     t.Steal,
			Guest:     t.Guest,
			GuestNice: t.GuestNice,
			Stolen:    t.Stolen,
		}
		updateStatsMap(sysCpuStats, s, fieldNames)
	}
	go func() {
		for {
			updateStats()
			time.Sleep(10 * time.Second)
		}
	}()
}

func exportSysDisk() {
	strPaths := os.Getenv(ref.EnvSysStatsDiskPaths)
	if strPaths == "" {
		return
	}
	for _, path := range strings.Split(strPaths, ",") {
		sysDiskStats := stats.NewMap(fmt.Sprintf("system/sysdisk/%s", naming.EncodeAsNameElement(path)))

		// Get field names to export.
		var s disk.UsageStat
		fieldNames := getFieldNames(s)

		// Update stats now and every 10 seconds afterwards.
		curPath := path
		updateStats := func() {
			if s, err := disk.Usage(curPath); err == nil {
				updateStatsMap(sysDiskStats, *s, fieldNames)
			}
		}
		go func() {
			for {
				updateStats()
				time.Sleep(10 * time.Second)
			}
		}()
	}
}

func getFieldNames(i interface{}) []string {
	fieldNames := []string{}
	v := reflect.ValueOf(i)
	v.FieldByNameFunc(func(name string) bool {
		switch v.FieldByName(name).Kind() {
		case reflect.Bool, reflect.Uint32, reflect.Uint64, reflect.Float64:
			fieldNames = append(fieldNames, name)
		}
		return false
	})
	return fieldNames
}

func updateStatsMap(m *stats.Map, s interface{}, fieldNames []string) {
	v := reflect.ValueOf(s)
	kv := make([]stats.KeyValue, len(fieldNames))
	for i, name := range fieldNames {
		kv[i] = stats.KeyValue{
			Key:   name,
			Value: v.FieldByName(name).Interface(),
		}
	}
	m.Set(kv)
}
