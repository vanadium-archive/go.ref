package benchmark

import "testing"

func Benchmark_dial_VIF(b *testing.B)     { benchmarkVIFDial(b, securityNone) }
func Benchmark_dial_VIF_TLS(b *testing.B) { benchmarkVIFDial(b, securityTLS) }

// Note: We don't benchmark Non-TLC VC Dial for now since it doesn't wait ack
// from the server after sending "OpenVC".
func Benchmark_dial_VC_TLS(b *testing.B) { benchmarkVCDial(b, securityTLS) }
