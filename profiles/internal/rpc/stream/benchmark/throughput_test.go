// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package benchmark

import "testing"

func Benchmark_throughput_TCP_1Conn(b *testing.B)  { benchmarkTCP(b, 1) }
func Benchmark_throughput_TCP_2Conns(b *testing.B) { benchmarkTCP(b, 2) }
func Benchmark_throughput_TCP_4Conns(b *testing.B) { benchmarkTCP(b, 4) }
func Benchmark_throughput_TCP_8Conns(b *testing.B) { benchmarkTCP(b, 8) }

func Benchmark_throughput_WS_1Conn(b *testing.B)  { benchmarkWS(b, 1) }
func Benchmark_throughput_WS_2Conns(b *testing.B) { benchmarkWS(b, 2) }
func Benchmark_throughput_WS_4Conns(b *testing.B) { benchmarkWS(b, 4) }
func Benchmark_throughput_WS_8Conns(b *testing.B) { benchmarkWS(b, 8) }

func Benchmark_throughput_WSH_TCP_1Conn(b *testing.B)  { benchmarkWSH(b, "tcp", 1) }
func Benchmark_throughput_WSH_TCP_2Conns(b *testing.B) { benchmarkWSH(b, "tcp", 2) }
func Benchmark_throughput_WSH_TCP_4Conns(b *testing.B) { benchmarkWSH(b, "tcp", 4) }
func Benchmark_throughput_WSH_TCP_8Conns(b *testing.B) { benchmarkWSH(b, "tcp", 8) }

func Benchmark_throughput_WSH_WS_1Conn(b *testing.B)  { benchmarkWSH(b, "ws", 1) }
func Benchmark_throughput_WSH_WS_2Conns(b *testing.B) { benchmarkWSH(b, "ws", 2) }
func Benchmark_throughput_WSH_WS_4Conns(b *testing.B) { benchmarkWSH(b, "ws", 4) }
func Benchmark_throughput_WSH_WS_8Conns(b *testing.B) { benchmarkWSH(b, "ws", 8) }

func Benchmark_throughput_Pipe_1Conn(b *testing.B)  { benchmarkPipe(b, 1) }
func Benchmark_throughput_Pipe_2Conns(b *testing.B) { benchmarkPipe(b, 2) }
func Benchmark_throughput_Pipe_4Conns(b *testing.B) { benchmarkPipe(b, 4) }
func Benchmark_throughput_Pipe_8Conns(b *testing.B) { benchmarkPipe(b, 8) }

func Benchmark_throughput_Flow_1VIF_1VC_1Flow_NoSecurity(b *testing.B) {
	benchmarkFlow(b, securityNone, 1, 1, 1)
}
func Benchmark_throughput_Flow_1VIF_1VC_2Flow_NoSecurity(b *testing.B) {
	benchmarkFlow(b, securityNone, 1, 1, 2)
}
func Benchmark_throughput_Flow_1VIF_1VC_8Flow_NoSecurity(b *testing.B) {
	benchmarkFlow(b, securityNone, 1, 1, 8)
}

func Benchmark_throughput_Flow_1VIF_2VC_2Flow_NoSecurity(b *testing.B) {
	benchmarkFlow(b, securityNone, 1, 2, 1)
}
func Benchmark_throughput_Flow_1VIF_2VC_8Flow_NoSecurity(b *testing.B) {
	benchmarkFlow(b, securityNone, 1, 2, 4)
}

func Benchmark_throughput_Flow_2VIF_4VC_8Flow_NoSecurity(b *testing.B) {
	benchmarkFlow(b, securityNone, 2, 2, 2)
}

func Benchmark_throughput_TLS_1Conn(b *testing.B)  { benchmarkTLS(b, 1) }
func Benchmark_throughput_TLS_2Conns(b *testing.B) { benchmarkTLS(b, 2) }
func Benchmark_throughput_TLS_4Conns(b *testing.B) { benchmarkTLS(b, 4) }
func Benchmark_throughput_TLS_8Conns(b *testing.B) { benchmarkTLS(b, 8) }

func Benchmark_throughput_Flow_1VIF_1VC_1Flow(b *testing.B) {
	benchmarkFlow(b, securityDefault, 1, 1, 1)
}
func Benchmark_throughput_Flow_1VIF_1VC_2Flow(b *testing.B) {
	benchmarkFlow(b, securityDefault, 1, 1, 2)
}
func Benchmark_throughput_Flow_1VIF_1VC_8Flow(b *testing.B) {
	benchmarkFlow(b, securityDefault, 1, 1, 8)
}

func Benchmark_throughput_Flow_1VIF_2VC_2Flow(b *testing.B) {
	benchmarkFlow(b, securityDefault, 1, 2, 1)
}
func Benchmark_throughput_Flow_1VIF_2VC_8Flow(b *testing.B) {
	benchmarkFlow(b, securityDefault, 1, 2, 4)
}

func Benchmark_throughput_Flow_2VIF_4VC_8Flow(b *testing.B) {
	benchmarkFlow(b, securityDefault, 2, 2, 2)
}
