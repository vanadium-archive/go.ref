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

Benchmark____1B     2000           5144219 ns/op           0.00 MB/s
Benchmark___10B     2000           5526448 ns/op           0.00 MB/s
Benchmark___1KB     2000           4528221 ns/op           0.44 MB/s
Benchmark_100KB     1000           7569096 ns/op          26.42 MB/s
Benchmark____1_chunk_____1B         1000           8945290 ns/op           0.00 MB/s
Benchmark____1_chunk____10B         1000           9711084 ns/op           0.00 MB/s
Benchmark____1_chunk____1KB         1000           8541689 ns/op           0.23 MB/s
Benchmark____1_chunk___10KB         1000           8972995 ns/op           2.23 MB/s
Benchmark___10_chunks____1B         1000          13114807 ns/op           0.00 MB/s
Benchmark___10_chunks___10B         1000          13219493 ns/op           0.02 MB/s
Benchmark___10_chunks___1KB         1000          13292236 ns/op           1.50 MB/s
Benchmark___10_chunks__10KB          500          15733197 ns/op          12.71 MB/s
Benchmark__100_chunks____1B          500          45078939 ns/op           0.00 MB/s
Benchmark__100_chunks___10B          200          49113585 ns/op           0.04 MB/s
Benchmark__100_chunks___1KB          100          57982457 ns/op           3.45 MB/s
Benchmark__100_chunks__10KB          100          81632487 ns/op          24.50 MB/s
Benchmark__per_chunk____1B         50000            357880 ns/op           0.01 MB/s
Benchmark__per_chunk___10B         20000            476941 ns/op           0.04 MB/s
Benchmark__per_chunk___1KB         10000            806491 ns/op           2.48 MB/s
Benchmark__per_chunk__10KB         10000           1185081 ns/op          16.88 MB/s
Benchmark____1B_mux___10_chunks___10B       1000          20235386 ns/op           0.00 MB/s
Benchmark____1B_mux___10_chunks___1KB        500          21346428 ns/op           0.00 MB/s
Benchmark____1B_mux__100_chunks___10B        100          72942436 ns/op           0.00 MB/s
Benchmark____1B_mux__100_chunks___1KB        100          81538481 ns/op           0.00 MB/s

'Benchmark___1KB' shows that it takes an average of 4.528 ms to
execute a simple Echo RPC with a 1 KB payload.

'Benchmark___10_chunks___1KB' shows that a streaming RPC with the
same payload (i.e. 10 chunks of 1 KB) takes about 13.292 ms on average.

'Benchmark__per_chunk___1KB' shows that sending a stream of 1 KB chunks
takes an average of 0.806 ms per chunk.

'Benchmark____1B_mux___10_chunks___1KB' shows that it takes an average
of 21.346 ms to execute a simple Echo RPC with a 1 B payload while streaming
10 chunks of 1 KB payloads continuously in the same process.

bm/main.go does the same benchmarks as ipc_test.go but with more varying
configurations and optional histogram outputs.

$ veyron go run veyron/runtimes/google/ipc/benchmarks/bm/main.go \
  -test.cpu=1,2 -test.benchtime=5s -histogram

RESULTS.txt has the latest benchmark results with main.go

================================================================================

bmserver/main.go and bmclient/main.go are simple command-line tools to run the
benchmark server and client as separate processes. Unlike the benchmarks above,
this test includes the startup cost of name resolution, creating the VC, etc. in
the first RPC.

$ veyron go run veyron/runtimes/google/ipc/benchmarks/bmserver/main.go \
  -veyron.tcp.address=localhost:8888 -acl='{"In":{"...":"R"}}'

(In a different shell)
$ veyron go run veyron/runtimes/google/ipc/benchmarks/bmclient/main.go \
  -server=/localhost:8888 -iterations=100 -chunk_count=0 -payload_size=10
iterations: 100  chunk_count: 0  payload_size: 10
elapsed time: 619.75741ms
Histogram (unit: ms)
Count: 100  Min: 4  Max: 54  Avg: 5.65
------------------------------------------------------------
[  4,   5)   42   42.0%   42.0%  ####
[  5,   6)   32   32.0%   74.0%  ###
[  6,   7)    8    8.0%   82.0%  #
[  7,   9)   13   13.0%   95.0%  #
[  9,  11)    3    3.0%   98.0%  
[ 11,  14)    1    1.0%   99.0%  
[ 14,  18)    0    0.0%   99.0%  
[ 18,  24)    0    0.0%   99.0%  
[ 24,  32)    0    0.0%   99.0%  
[ 32,  42)    0    0.0%   99.0%  
[ 42,  55)    1    1.0%  100.0%  
[ 55,  72)    0    0.0%  100.0%  
[ 72,  94)    0    0.0%  100.0%  
[ 94, 123)    0    0.0%  100.0%  
[123, 161)    0    0.0%  100.0%  
[161, 211)    0    0.0%  100.0%  
[211, inf)    0    0.0%  100.0%  


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
