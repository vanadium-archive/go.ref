#!/bin/sh

# Test the identity command-line tool.
#
# This tests most operations of the identity command-line tool.
# Not the "seekblessing" command yet, since that requires
# starting a separate server.

toplevel=$(git rev-parse --show-toplevel)
go=${toplevel}/scripts/build/go
thisscript=$0


workdir=$(mktemp -d ${toplevel}/go/tmp.XXXXXXXXXX)
export TMPDIR=$workdir
trap onexit EXIT

onexit() {
	cd /
	exec 2> /dev/null
	rm -rf $workdir
}

FAIL() {
	[ $# -gt 0 ] && echo "$thisscript $*"
	echo FAIL
	exit 1
}

PASS() {
	echo PASS
	exit 0
}

# Build binaries.
cd $workdir
$go build veyron/tools/identity || FAIL "line $LINENO: failed to build identity"

./identity print >/dev/null || FAIL "line $LINENO: print failed"
./identity generate >/dev/null || FAIL "line $LINENO: generate failed"
./identity generate root >root || FAIL "line $LINENO: generate root failed"

export VEYRON_IDENTITY="root"

# Generate an identity and get it blessed by root using "identity bless"
got=$(./identity generate ignoreme | ./identity bless - child | ./identity print - | awk '/Name/ {print $3}')
want="root/child"
if [ "$got" != "$want" ]; then
	FAIL "line $LINENO: Got $got, want $want"
fi

# Generate an identity and get it blessed by root using "identity bless --with"
./identity generate other >other || FAIL
got=$(./identity generate ignoreme | ./identity bless --with=other - child | ./identity print - | awk '/Name/ {print $3}')
want="unknown/other/child"
if [ "$got" != "$want" ]; then
	FAIL "line $LINENO: Got $got, want $want"
fi
PASS
