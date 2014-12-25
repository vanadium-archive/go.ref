package rt

import (
	"v.io/veyron/veyron2/vlog"
)

func (*vrt) Logger() vlog.Logger {
	return vlog.Log
}

func (*vrt) NewLogger(name string, opts ...vlog.LoggingOpts) (vlog.Logger, error) {
	return vlog.NewLogger(name, opts...)
}

// initLogging configures logging for the runtime. It needs to be called after
// flag.Parse and after signal handling has been initialized.
func (*vrt) initLogging() error {
	return vlog.ConfigureLibraryLoggerFromFlags()
}

func (*vrt) shutdownLogging() {
	vlog.FlushLog()
}
