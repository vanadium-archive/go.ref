// Package impl is the server-side implementation of the pprof interface.
package impl

import (
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/verror"
)

// NewPProfService returns a new pprof service implementation.
func NewPProfService() interface{} {
	return &pprofService{}
}

type pprofService struct {
}

// CmdLine returns the command-line argument of the server.
func (pprofService) CmdLine(ipc.ServerContext) ([]string, error) {
	return os.Args, nil
}

// Profiles returns the list of available profiles.
func (pprofService) Profiles(ipc.ServerContext) ([]string, error) {
	profiles := pprof.Profiles()
	results := make([]string, len(profiles))
	for i, v := range profiles {
		results[i] = v.Name()
	}
	return results, nil
}

// Profile streams the requested profile. The debug parameter enables
// additional output. Passing debug=0 includes only the hexadecimal
// addresses that pprof needs. Passing debug=1 adds comments translating
// addresses to function names and line numbers, so that a programmer
// can read the profile without tools.
//
// TODO(toddw): Change ipc.ServerCall into a struct stub context.
func (pprofService) Profile(call ipc.ServerCall, name string, debug int32) error {
	profile := pprof.Lookup(name)
	if profile == nil {
		return verror.NoExistf("profile does not exist")
	}
	if err := profile.WriteTo(&streamWriter{call}, int(debug)); err != nil {
		return verror.Convert(err)
	}
	return nil
}

// CPUProfile enables CPU profiling for the requested duration and
// streams the profile data.
//
// TODO(toddw): Change ipc.ServerCall into a struct stub context.
func (pprofService) CPUProfile(call ipc.ServerCall, seconds int32) error {
	if seconds <= 0 || seconds > 3600 {
		return verror.BadArgf("invalid number of seconds: %d", seconds)
	}
	if err := pprof.StartCPUProfile(&streamWriter{call}); err != nil {
		return verror.Convert(err)
	}
	time.Sleep(time.Duration(seconds) * time.Second)
	pprof.StopCPUProfile()
	return nil
}

// Symbol looks up the program counters and returns their respective
// function names.
func (pprofService) Symbol(_ ipc.ServerContext, programCounters []uint64) ([]string, error) {
	results := make([]string, len(programCounters))
	for i, v := range programCounters {
		f := runtime.FuncForPC(uintptr(v))
		if f != nil {
			results[i] = f.Name()
		}
	}
	return results, nil
}

type streamWriter struct {
	call ipc.ServerCall
}

func (w *streamWriter) Write(p []byte) (int, error) {
	if err := w.call.Send(p); err != nil {
		return 0, err
	}
	return len(p), nil
}
