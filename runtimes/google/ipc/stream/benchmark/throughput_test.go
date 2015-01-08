package benchmark

import "testing"

func Benchmark_throughput_TCP_1Conn(b *testing.B)  { benchmarkTCP(b, 1) }
func Benchmark_throughput_TCP_2Conns(b *testing.B) { benchmarkTCP(b, 2) }
func Benchmark_throughput_TCP_4Conns(b *testing.B) { benchmarkTCP(b, 4) }
func Benchmark_throughput_TCP_8Conns(b *testing.B) { benchmarkTCP(b, 8) }

func Benchmark_throughput_Pipe_1Conn(b *testing.B)  { benchmarkPipe(b, 1) }
func Benchmark_throughput_Pipe_2Conns(b *testing.B) { benchmarkPipe(b, 2) }
func Benchmark_throughput_Pipe_4Conns(b *testing.B) { benchmarkPipe(b, 4) }
func Benchmark_throughput_Pipe_8Conns(b *testing.B) { benchmarkPipe(b, 8) }

func Benchmark_throughput_Flow_1VIF_1VC_1Flow(b *testing.B) { benchmarkFlow(b, securityNone, 1, 1, 1) }
func Benchmark_throughput_Flow_1VIF_1VC_2Flow(b *testing.B) { benchmarkFlow(b, securityNone, 1, 1, 2) }
func Benchmark_throughput_Flow_1VIF_1VC_8Flow(b *testing.B) { benchmarkFlow(b, securityNone, 1, 1, 8) }

func Benchmark_throughput_Flow_1VIF_2VC_2Flow(b *testing.B) { benchmarkFlow(b, securityNone, 1, 2, 1) }
func Benchmark_throughput_Flow_1VIF_2VC_8Flow(b *testing.B) { benchmarkFlow(b, securityNone, 1, 2, 4) }

func Benchmark_throughput_Flow_2VIF_4VC_8Flow(b *testing.B) { benchmarkFlow(b, securityNone, 2, 2, 2) }

func Benchmark_throughput_TLS_1Conn(b *testing.B)  { benchmarkTLS(b, 1) }
func Benchmark_throughput_TLS_2Conns(b *testing.B) { benchmarkTLS(b, 2) }
func Benchmark_throughput_TLS_4Conns(b *testing.B) { benchmarkTLS(b, 4) }
func Benchmark_throughput_TLS_8Conns(b *testing.B) { benchmarkTLS(b, 8) }

func Benchmark_throughput_Flow_1VIF_1VC_1FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 1, 1, 1) }
func Benchmark_throughput_Flow_1VIF_1VC_2FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 1, 1, 2) }
func Benchmark_throughput_Flow_1VIF_1VC_8FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 1, 1, 8) }

func Benchmark_throughput_Flow_1VIF_2VC_2FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 1, 2, 1) }
func Benchmark_throughput_Flow_1VIF_2VC_8FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 1, 2, 4) }

func Benchmark_throughput_Flow_2VIF_4VC_8FlowTLS(b *testing.B) { benchmarkFlow(b, securityTLS, 2, 2, 2) }
