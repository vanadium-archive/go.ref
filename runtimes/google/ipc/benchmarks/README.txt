This directory contains code uses to measure the performance of the Veyron IPC
stack.

The ipc_test.go file uses GO's testing package to run benchmarks. Each
benchmark involves one server and one client. The server has two very simple
methods that echo the data received from the client back to the client.

client ---- Echo(payload) ----> server
client <--- return payload ---- server

There are two versions of the Echo method:
 - Echo(payload []byte) ([]byte], error)
 - EchoStream() <[]byte,[]byte> error

The first benchmarks use the non-streaming version of Echo with a varying
payload size. The other benchmarks use the streaming version with varying
number of chunks and payload sizes.

All these benchmarks create a VC before measurements begin. So, the VC creation
overhead is excluded.

On a ThinkPad X1 Carbon (2 × Intel(R) Core(TM) i7-3667U CPU @ 2.00GHz), we get:

$ veyron go test -test.bench=. -test.cpu=1 \
	-test.benchtime=5s veyron/runtimes/google/ipc/benchmarks 2> /dev/null
PASS
Benchmark____1B	   10000	    545077 ns/op
Benchmark___10B	   10000	    587312 ns/op
Benchmark__100B	   10000	    523019 ns/op
Benchmark___1KB	   10000	    605235 ns/op
Benchmark__10KB	   10000	    957467 ns/op
Benchmark_100KB	    5000	   4101891 ns/op
Benchmark_N_RPCs____1_chunk_____1B	   10000	    554063 ns/op
Benchmark_N_RPCs____1_chunk____10B	   10000	    551424 ns/op
Benchmark_N_RPCs____1_chunk___100B	   10000	    538308 ns/op
Benchmark_N_RPCs____1_chunk____1KB	   10000	    585579 ns/op
Benchmark_N_RPCs____1_chunk___10KB	   10000	    904789 ns/op
Benchmark_N_RPCs___10_chunks___1KB	   10000	   1460984 ns/op
Benchmark_N_RPCs__100_chunks___1KB	    1000	   8491514 ns/op
Benchmark_N_RPCs_1000_chunks___1KB	     100	 105269359 ns/op
Benchmark_1_RPC_N_chunks_____1B	  200000	    763769 ns/op
Benchmark_1_RPC_N_chunks____10B	  100000	    583134 ns/op
Benchmark_1_RPC_N_chunks___100B	  100000	     80849 ns/op
Benchmark_1_RPC_N_chunks____1KB	  100000	     88820 ns/op
Benchmark_1_RPC_N_chunks___10KB	   50000	    361596 ns/op
Benchmark_1_RPC_N_chunks__100KB	    5000	   3127193 ns/op
ok  	veyron/runtimes/google/ipc/benchmarks	525.095s


The Benchmark_Simple_____1KB line shows that it takes an average of 0.605 ms to
execute a simple Echo RPC with a 1 KB payload.

The Benchmark_N_RPCs____1_chunk____1KB line shows that a streaming RPC with the
same payload (i.e. 1 chunk of 1 KB) takes about 0.586 ms on average.

And Benchmark_1_RPC_N_chunks____1KB shows that sending a stream of 1 KB chunks
takes an average of 0.088 ms per chunk.


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

