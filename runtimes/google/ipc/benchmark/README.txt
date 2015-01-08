This directory contains code uses to measure the performance of the Vanadium IPC
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

This test creates a VC before the benchmark begins. So, the VC creation
overhead is excluded.

$ v23 go test -bench=. -timeout=1h -cpu=1 -benchtime=5s \
  v.io/core/veyron/runtimes/google/ipc/benchmark
PASS
Benchmark____1B	    1000	   8301357 ns/op	   0.00 MB/s
--- Histogram (unit: ms)
	Count: 1000  Min: 7  Max: 17  Avg: 7.89
	------------------------------------------------------------
	[  7,   8)   505   50.5%   50.5%  #####
	[  8,   9)   389   38.9%   89.4%  ####
	[  9,  10)    38    3.8%   93.2%  
	[ 10,  11)    12    1.2%   94.4%  
	[ 11,  12)     4    0.4%   94.8%  
	[ 12,  14)    19    1.9%   96.7%  
	[ 14,  16)    23    2.3%   99.0%  
	[ 16,  18)    10    1.0%  100.0%  
	[ 18,  21)     0    0.0%  100.0%  
	[ 21,  24)     0    0.0%  100.0%  
	[ 24, inf)     0    0.0%  100.0%  
Benchmark___10B	    1000	   8587341 ns/op	   0.00 MB/s
...

RESULTS.txt has the full benchmark results.

================================================================================

bmserver/main.go and bmclient/main.go are simple command-line tools to run the
benchmark server and client as separate processes. Unlike the benchmarks above,
this test includes the startup cost of name resolution, creating the VC, etc. in
the first RPC.

$ v23 go run bmserver/main.go \
  -veyron.tcp.address=localhost:8888 -acl='{"In":{"...":"R"}}'

(In a different shell)
$ v23 go run bmclient/main.go \
  -server=/localhost:8888 -iterations=100 -chunk_count=0 -payload_size=10
iterations: 100  chunk_count: 0  payload_size: 10
elapsed time: 1.369034277s
Histogram (unit: ms)
Count: 100  Min: 7  Max: 94  Avg: 13.17
------------------------------------------------------------
[  7,   8)    1    1.0%    1.0%  
[  8,   9)    4    4.0%    5.0%  
[  9,  10)   17   17.0%   22.0%  ##
[ 10,  12)   24   24.0%   46.0%  ##
[ 12,  15)   24   24.0%   70.0%  ##
[ 15,  19)   28   28.0%   98.0%  ###
[ 19,  24)    1    1.0%   99.0%  
[ 24,  32)    0    0.0%   99.0%  
[ 32,  42)    0    0.0%   99.0%  
[ 42,  56)    0    0.0%   99.0%  
[ 56,  75)    0    0.0%   99.0%  
[ 75, 101)    1    1.0%  100.0%  
[101, 136)    0    0.0%  100.0%  
[136, 183)    0    0.0%  100.0%  
[183, 247)    0    0.0%  100.0%  
[247, 334)    0    0.0%  100.0%  
[334, inf)    0    0.0%  100.0%  


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
