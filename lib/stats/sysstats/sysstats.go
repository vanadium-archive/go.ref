// Package sysstats exports system statistics, and updates them periodically.
// The package does not export any symbols, but needs to be imported for its
// side-effects.
package sysstats

import (
	"os"
	"reflect"
	"runtime"
	"strings"
	"time"

	"veyron.io/veyron/veyron/lib/stats"
)

func init() {
	now := time.Now()
	stats.NewInteger("system/start-time-unix").Set(now.Unix())
	stats.NewString("system/start-time-rfc1123").Set(now.Format(time.RFC1123))
	stats.NewString("system/cmdline").Set(strings.Join(os.Args, " "))
	stats.NewInteger("system/num-cpu").Set(int64(runtime.NumCPU()))
	stats.NewInteger("system/num-goroutine").Set(int64(runtime.NumGoroutine()))
	stats.NewString("system/version").Set(runtime.Version())
	if hostname, err := os.Hostname(); err == nil {
		stats.NewString("system/hostname").Set(hostname)
	}
	exportEnv()
	exportMemStats()
}

func exportEnv() {
	kv := make([]stats.KeyValue, len(os.Environ()))
	for i, v := range os.Environ() {
		if parts := strings.SplitN(v, "=", 2); len(parts) == 2 {
			kv[i] = stats.KeyValue{parts[0], parts[1]}
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
			kv[i] = stats.KeyValue{name, v.FieldByName(name).Interface()}
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
