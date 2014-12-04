package benchmark

import "testing"

func BenchmarkVIF_Dial(b *testing.B)    { benchmarkVIFDial(b, securityNone) }
func BenchmarkVIF_DialTLS(b *testing.B) { benchmarkVIFDial(b, securityTLS) }

// Note: We don't benchmark Non-TLC VC Dial for now since it doesn't wait ack
// from the server after sending "OpenVC".
func BenchmarkVC_DialTLS(b *testing.B) { benchmarkVCDial(b, securityTLS) }
