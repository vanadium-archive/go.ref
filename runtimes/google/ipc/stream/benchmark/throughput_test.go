package benchmark

import "testing"

func BenchmarkTCP_1Conn(b *testing.B)  { benchmarkTCP(b, 1) }
func BenchmarkTCP_2Conns(b *testing.B) { benchmarkTCP(b, 2) }
func BenchmarkTCP_4Conns(b *testing.B) { benchmarkTCP(b, 4) }
func BenchmarkTCP_8Conns(b *testing.B) { benchmarkTCP(b, 8) }

func BenchmarkPipe_1Conn(b *testing.B)  { benchmarkPipe(b, 1) }
func BenchmarkPipe_2Conns(b *testing.B) { benchmarkPipe(b, 2) }
func BenchmarkPipe_4Conns(b *testing.B) { benchmarkPipe(b, 4) }
func BenchmarkPipe_8Conns(b *testing.B) { benchmarkPipe(b, 8) }

func BenchmarkFlow_1VIF_1VC_1Flow(b *testing.B) { benchmarkFlow(b, securityNone, 1, 1, 1) }
func BenchmarkFlow_1VIF_1VC_2Flow(b *testing.B) { benchmarkFlow(b, securityNone, 1, 1, 2) }
func BenchmarkFlow_1VIF_1VC_8Flow(b *testing.B) { benchmarkFlow(b, securityNone, 1, 1, 8) }

func BenchmarkFlow_1VIF_2VC_2Flow(b *testing.B) { benchmarkFlow(b, securityNone, 1, 2, 1) }
func BenchmarkFlow_1VIF_2VC_8Flow(b *testing.B) { benchmarkFlow(b, securityNone, 1, 2, 4) }

func BenchmarkFlow_2VIF_4VC_8Flow(b *testing.B) { benchmarkFlow(b, securityNone, 2, 2, 2) }

func BenchmarkTLS_1Conn(b *testing.B)  { benchmarkTLS(b, 1) }
func BenchmarkTLS_2Conns(b *testing.B) { benchmarkTLS(b, 2) }
func BenchmarkTLS_4Conns(b *testing.B) { benchmarkTLS(b, 4) }
func BenchmarkTLS_8Conns(b *testing.B) { benchmarkTLS(b, 8) }

func BenchmarkFlow_1VIF_1VC_1FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 1, 1, 1) }
func BenchmarkFlow_1VIF_1VC_2FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 1, 1, 2) }
func BenchmarkFlow_1VIF_1VC_8FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 1, 1, 8) }

func BenchmarkFlow_1VIF_2VC_2FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 1, 2, 1) }
func BenchmarkFlow_1VIF_2VC_8FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 1, 2, 4) }

func BenchmarkFlow_2VIF_4VC_8FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 2, 2, 2) }

// A single empty test to avoid:
// testing: warning: no tests to run
// from showing up when running benchmarks in this package via "go test"
func TestNoOp(t *testing.T) {}
