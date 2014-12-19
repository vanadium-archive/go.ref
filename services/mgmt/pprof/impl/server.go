// Package impl is the server-side implementation of the pprof interface.
package impl

import (
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"veyron.io/veyron/veyron2/ipc"
	spprof "veyron.io/veyron/veyron2/services/mgmt/pprof"
	verror "veyron.io/veyron/veyron2/verror2"
)

const pkgPath = "veyron.io/veyron/veyron/services/mgmt/pprof/impl"

// Errors
var (
	errNoProfile      = verror.Register(pkgPath+".errNoProfile", verror.NoRetry, "{1:}{2:} profile does not exist{:_}")
	errInvalidSeconds = verror.Register(pkgPath+".errInvalidSeconds", verror.NoRetry, "{1:}{2:} invalid number of seconds{:_}")
)

// NewPProfService returns a new pprof service implementation.
func NewPProfService() interface{} {
	return spprof.PProfServer(&pprofService{})
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
func (pprofService) Profile(ctx spprof.PProfProfileContext, name string, debug int32) error {
	profile := pprof.Lookup(name)
	if profile == nil {
		return verror.Make(errNoProfile, ctx.Context(), name)
	}
	if err := profile.WriteTo(&streamWriter{ctx.SendStream()}, int(debug)); err != nil {
		return verror.Convert(verror.Unknown, ctx.Context(), err)
	}
	return nil
}

// CPUProfile enables CPU profiling for the requested duration and
// streams the profile data.
func (pprofService) CPUProfile(ctx spprof.PProfCPUProfileContext, seconds int32) error {
	if seconds <= 0 || seconds > 3600 {
		return verror.Make(errInvalidSeconds, ctx.Context(), seconds)
	}
	if err := pprof.StartCPUProfile(&streamWriter{ctx.SendStream()}); err != nil {
		return verror.Convert(verror.Unknown, ctx.Context(), err)
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
	sender interface {
		Send(item []byte) error
	}
}

func (w *streamWriter) Write(p []byte) (int, error) {
	if err := w.sender.Send(p); err != nil {
		return 0, err
	}
	return len(p), nil
}
