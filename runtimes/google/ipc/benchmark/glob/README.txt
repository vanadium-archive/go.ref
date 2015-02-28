Glob Benchmarks

The benchmarks in this directory attempt to provide some guidance for the amount
of buffering to use with the channels returned by Glob__ and GlobChildren__.

The first set of benchmarks (BenchmarkChanN) shows the relationship between the
buffer size and the latency of a very simple channel with one writer and one
reader doing nothing else.

The second set of benchmarks (BenchmarkGlobN) does the same thing but with a
Glob__ server and a Glob client. The third set (BenchmarkGlobChildrenN) uses
GlobChildren__.

As of 2014-12-03, the conclusion is that the queue size has very little impact
on performance.

The BenchmarkChanN set shows that increasing the queue size improves latency for
the very simple case, but not for Glob__ or GlobChildren__.

An interesting observation is that all the benchmarks get slower as the number
of cpus increases.

Below are the test results for 1, 2, and 4 cpus on a HP Z420 workstation with
2 × 6 cpu cores (Intel(R) Xeon(R) CPU E5-1650 v2 @ 3.50GHz).

$ ./glob.test -test.bench=. -test.benchtime=5s -test.cpu=1
BenchmarkChan0	20000000	       464 ns/op
BenchmarkChan1	20000000	       585 ns/op
BenchmarkChan2	20000000	       484 ns/op
BenchmarkChan4	20000000	       425 ns/op
BenchmarkChan8	50000000	       396 ns/op
BenchmarkChan16	50000000	       381 ns/op
BenchmarkChan32	50000000	       371 ns/op
BenchmarkChan64	50000000	       365 ns/op
BenchmarkChan128	50000000	       363 ns/op
BenchmarkChan256	50000000	       362 ns/op
BenchmarkGlob0	  500000	     35029 ns/op
BenchmarkGlob1	  500000	     63536 ns/op
BenchmarkGlob2	  500000	     34753 ns/op
BenchmarkGlob4	  500000	     26379 ns/op
BenchmarkGlob8	  500000	     19293 ns/op
BenchmarkGlob16	 1000000	     18149 ns/op
BenchmarkGlob32	  500000	     52364 ns/op
BenchmarkGlob64	  500000	     83879 ns/op
BenchmarkGlob128	  100000	     88448 ns/op
BenchmarkGlob256	  100000	     57922 ns/op
BenchmarkGlobChildren0	  100000	    118448 ns/op
BenchmarkGlobChildren1	  100000	    123274 ns/op
BenchmarkGlobChildren2	  100000	    116110 ns/op
BenchmarkGlobChildren4	  100000	    134175 ns/op
BenchmarkGlobChildren8	  100000	    118776 ns/op
BenchmarkGlobChildren16	  100000	    123191 ns/op
BenchmarkGlobChildren32	  100000	    132195 ns/op
BenchmarkGlobChildren64	  100000	    126004 ns/op
BenchmarkGlobChildren128	  100000	    135072 ns/op
BenchmarkGlobChildren256	  100000	    127399 ns/op

$ ./glob.test -test.bench=. -test.benchtime=5s -test.cpu=2
BenchmarkChan0-2	 5000000	      1595 ns/op
BenchmarkChan1-2	 5000000	      1649 ns/op
BenchmarkChan2-2	10000000	      1245 ns/op
BenchmarkChan4-2	10000000	      1299 ns/op
BenchmarkChan8-2	10000000	       982 ns/op
BenchmarkChan16-2	10000000	       929 ns/op
BenchmarkChan32-2	10000000	       916 ns/op
BenchmarkChan64-2	10000000	       903 ns/op
BenchmarkChan128-2	10000000	       907 ns/op
BenchmarkChan256-2	10000000	       914 ns/op
BenchmarkGlob0-2	  500000	     61455 ns/op
BenchmarkGlob1-2	  500000	     46890 ns/op
BenchmarkGlob2-2	  200000	     56462 ns/op
BenchmarkGlob4-2	  500000	     22783 ns/op
BenchmarkGlob8-2	  200000	     64783 ns/op
BenchmarkGlob16-2	 1000000	     68119 ns/op
BenchmarkGlob32-2	  200000	     78611 ns/op
BenchmarkGlob64-2	  500000	     82180 ns/op
BenchmarkGlob128-2	 1000000	     81548 ns/op
BenchmarkGlob256-2	  100000	     88278 ns/op
BenchmarkGlobChildren0-2	  100000	     83188 ns/op
BenchmarkGlobChildren1-2	  100000	     81751 ns/op
BenchmarkGlobChildren2-2	  100000	     81896 ns/op
BenchmarkGlobChildren4-2	  100000	     81857 ns/op
BenchmarkGlobChildren8-2	  100000	     81531 ns/op
BenchmarkGlobChildren16-2	  100000	     89915 ns/op
BenchmarkGlobChildren32-2	  100000	     81112 ns/op
BenchmarkGlobChildren64-2	  100000	     80997 ns/op
BenchmarkGlobChildren128-2	  100000	     81350 ns/op
BenchmarkGlobChildren256-2	  100000	     81344 ns/op

$ ./glob.test -test.bench=. -test.benchtime=5s -test.cpu=4
BenchmarkChan0-4	 5000000	      2012 ns/op
BenchmarkChan1-4	 5000000	      3149 ns/op
BenchmarkChan2-4	 5000000	      1839 ns/op
BenchmarkChan4-4	10000000	       957 ns/op
BenchmarkChan8-4	20000000	       660 ns/op
BenchmarkChan16-4	20000000	       523 ns/op
BenchmarkChan32-4	20000000	       507 ns/op
BenchmarkChan64-4	20000000	       509 ns/op
BenchmarkChan128-4	20000000	       507 ns/op
BenchmarkChan256-4	20000000	       511 ns/op
BenchmarkGlob0-4	  100000	    103269 ns/op
BenchmarkGlob1-4	  100000	    101222 ns/op
BenchmarkGlob2-4	  100000	    102049 ns/op
BenchmarkGlob4-4	  100000	    102763 ns/op
BenchmarkGlob8-4	  100000	    101939 ns/op
BenchmarkGlob16-4	  100000	    102989 ns/op
BenchmarkGlob32-4	  100000	    103898 ns/op
BenchmarkGlob64-4	  100000	    102838 ns/op
BenchmarkGlob128-4	  100000	    101532 ns/op
BenchmarkGlob256-4	  100000	    101059 ns/op
BenchmarkGlobChildren0-4	  100000	    106617 ns/op
BenchmarkGlobChildren1-4	  100000	    102576 ns/op
BenchmarkGlobChildren2-4	  100000	    106313 ns/op
BenchmarkGlobChildren4-4	  100000	    102774 ns/op
BenchmarkGlobChildren8-4	  100000	    102886 ns/op
BenchmarkGlobChildren16-4	  100000	    106771 ns/op
BenchmarkGlobChildren32-4	  100000	    103309 ns/op
BenchmarkGlobChildren64-4	  100000	    105112 ns/op
BenchmarkGlobChildren128-4	  100000	    102295 ns/op
BenchmarkGlobChildren256-4	  100000	    102951 ns/op
