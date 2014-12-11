This directory contains code uses to measure the performance of the Veyron IPC
stack.

================================================================================

The ipc_test.go file uses GO's testing package to run benchmarks. Each
benchmark involves one server and one client. The server has two very simple
methods that echo the data received from the client back to the client.

client ---- Echo(payload) ----> server
client <--- return payload ---- server

There are two versions of the Echo method:
 - Echo(payload []byte) ([]byte], error)
 - EchoStream() <[]byte,[]byte> error

The first benchmarks use the non-streaming version of Echo with a varying
payload size. The second benchmarks use the streaming version with varying
number of chunks and payload sizes. The third one is for measuring the
performance with multiple clients hosted in the same process.

All these benchmarks create a VC before measurements begin. So, the VC creation
overhead is excluded.

$ veyron go test -test.bench=. -timeout=1h -test.cpu=1 -test.benchtime=5s \
  veyron/runtimes/google/ipc/benchmarks

Benchmark____1B     2000     3895391 ns/op     0.00 MB/s
Benchmark___10B     2000     3982372 ns/op     0.01 MB/s
Benchmark___1KB     5000     3251297 ns/op     0.62 MB/s
Benchmark_100KB     2000     6244664 ns/op    32.03 MB/s
Benchmark____1_chunk_____1B     5000     4070866 ns/op     0.00 MB/s
Benchmark____1_chunk____10B     2000     4242328 ns/op     0.00 MB/s
Benchmark____1_chunk____1KB     2000     3679679 ns/op     0.54 MB/s
Benchmark____1_chunk___10KB     2000     4070936 ns/op     4.91 MB/s
Benchmark___10_chunks____1B     2000     3828552 ns/op     0.01 MB/s
Benchmark___10_chunks___10B     5000     3685269 ns/op     0.05 MB/s
Benchmark___10_chunks___1KB     2000     6831116 ns/op     2.93 MB/s
Benchmark___10_chunks__10KB     1000     9662880 ns/op    20.70 MB/s
Benchmark__100_chunks____1B     2000     8938980 ns/op     0.02 MB/s
Benchmark__100_chunks___10B     2000     5924969 ns/op     0.34 MB/s
Benchmark__100_chunks___1KB      500    37264103 ns/op     5.37 MB/s
Benchmark__100_chunks__10KB      100    64999728 ns/op    30.77 MB/s
Benchmark__per_chunk____1B    500000     1535312 ns/op     0.00 MB/s
Benchmark__per_chunk___10B      2000     9416017 ns/op     0.00 MB/s
Benchmark__per_chunk___1KB      1000     7803789 ns/op     0.26 MB/s
Benchmark__per_chunk__10KB      1000     7828585 ns/op     2.55 MB/s
Benchmark____1B_mux___10_chunks___10B     1000     9233379 ns/op     0.00 MB/s
Benchmark____1B_mux___10_chunks___1KB     1000     8639613 ns/op     0.00 MB/s
Benchmark____1B_mux__100_chunks___10B      500    30530925 ns/op     0.00 MB/s
Benchmark____1B_mux__100_chunks___1KB      200    40886630 ns/op     0.00 MB/s

'Benchmark___1KB' shows that it takes an average of 3.251 ms to
execute a simple Echo RPC with a 1 KB payload.

'Benchmark___10_chunks___1KB' shows that a streaming RPC with the
same payload (i.e. 10 chunks of 1 KB) takes about 6.831 ms on average.

'Benchmark__per_chunk___1KB' shows that sending a stream of 1 KB chunks
takes an average of 7.804 ms per chunk.

'Benchmark____1B_mux___10_chunks___1KB' shows that it takes an average
of 9.233 ms to execute a simple Echo RPC with a 1 B payload while streaming
10 chunks of 1 KB payloads continuously in the same process.

bm/main.go does the same benchmarks as ipc_test.go but with more varying
configurations and optional histogram outputs.

$ veyron go run veyron/runtimes/google/ipc/benchmarks/bm/main.go \
  -test.cpu=1,2 -test.benchtime=5s -histogram

RESULTS.txt has the latest benchmark results with main.go

================================================================================

Running the client and server as separate processes.

In this case, we can see the cost of name resolution, creating the VC, etc. in
the first RPC.

$ $VEYRON_ROOT/veyron/go/bin/bmserver --address=localhost:8888 --acl='{"...":"A"}'

(In a different shell)
$ $VEYRON_ROOT/veyron/go/bin/bmclient --server=/localhost:8888 --count=10 \
	--payload_size=1000
CallEcho 0 64133467
CallEcho 1 766223
CallEcho 2 703860
CallEcho 3 697590
CallEcho 4 601134
CallEcho 5 601142
CallEcho 6 624079
CallEcho 7 644664
CallEcho 8 605195
CallEcho 9 637037

It took about 64 ms to execute the first RPC, and then 0.60-0.70 ms to execute
the next ones.


On a Raspberry Pi, everything is much slower. The same tests show the following
results:

$ ./benchmarks.test -test.bench=. -test.cpu=1 -test.benchtime=5s 2>/dev/null
PASS
Benchmark____1B             500          21316148 ns/op
Benchmark___10B             500          23304638 ns/op
Benchmark__100B             500          21860446 ns/op
Benchmark___1KB             500          24000346 ns/op
Benchmark__10KB             200          37530575 ns/op
Benchmark_100KB             100         136243310 ns/op
Benchmark_N_RPCs____1_chunk_____1B           500          19957506 ns/op
Benchmark_N_RPCs____1_chunk____10B           500          22868392 ns/op
Benchmark_N_RPCs____1_chunk___100B           500          19635412 ns/op
Benchmark_N_RPCs____1_chunk____1KB           500          22572190 ns/op
Benchmark_N_RPCs____1_chunk___10KB           500          37570948 ns/op
Benchmark_N_RPCs___10_chunks___1KB           100          51670740 ns/op
Benchmark_N_RPCs__100_chunks___1KB            50         364938740 ns/op
Benchmark_N_RPCs_1000_chunks___1KB             2        3586374500 ns/op
Benchmark_1_RPC_N_chunks_____1B    10000           1034042 ns/op
Benchmark_1_RPC_N_chunks____10B     5000           1894875 ns/op
Benchmark_1_RPC_N_chunks___100B     5000           2857289 ns/op
Benchmark_1_RPC_N_chunks____1KB     5000           6465839 ns/op
Benchmark_1_RPC_N_chunks___10KB      100          80019430 ns/op
Benchmark_1_RPC_N_chunks__100KB Killed

The simple 1 KB RPCs take an average of 24 ms. The streaming equivalent takes
about 22 ms, and streaming many 1 KB chunks takes about 6.5 ms per chunk.


$ ./bmserver --address=localhost:8888 --acl='{"...":"A"}'

$ ./bmclient --server=/localhost:8888 --count=10 --payload_size=1000
CallEcho 0 2573406000
CallEcho 1 44669000
CallEcho 2 54442000
CallEcho 3 33934000
CallEcho 4 47985000
CallEcho 5 61324000
CallEcho 6 51654000
CallEcho 7 47043000
CallEcho 8 44995000
CallEcho 9 53166000

On the pi, the first RPC takes ~2.5 sec to execute.
