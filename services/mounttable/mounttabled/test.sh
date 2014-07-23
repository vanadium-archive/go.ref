#!/bin/sh

# Test the mounttabled binary
#
# This test starts a mounttable server with the neighborhood option enabled,
# and mounts 1) the mounttable on itself as 'myself', and 2) www.google.com:80
# as 'google'.
#
# Then it verifies that <mounttable>.Glob(*) and <neighborhood>.Glob(nhname)
# return the correct result.

toplevel=$(git rev-parse --show-toplevel)
go=${toplevel}/scripts/build/go
thisscript=$0

echo "Test directory: $(dirname $0)"

workdir=$(mktemp -d ${toplevel}/go/tmp.XXXXXXXXXX)
export TMPDIR=$workdir
trap onexit EXIT

onexit() {
	cd /
	exec 2> /dev/null
	kill -9 $(jobs -p)
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

# Build mounttabled and mounttable binaries.
cd $workdir
$go build veyron/services/mounttable/mounttabled || FAIL "line $LINENO: failed to build mounttabled"
$go build veyron/tools/mounttable || FAIL "line $LINENO: failed to build mounttable"

# Start mounttabled and find its endpoint.
nhname=test$$
mtlog=$workdir/mt.log
./mounttabled --address=localhost:0 --neighborhood_name=$nhname > $mtlog 2>&1 &

for i in 1 2 3 4; do
	ep=$(grep "Mount table service at:" $mtlog | sed -re 's/^.*endpoint: ([^ ]*).*/\1/')
	if [ -n "$ep" ]; then
		break
	fi
	sleep 1
done

[ -z $ep ] && FAIL "line $LINENO: no server"

# Get the neighborhood endpoint from the mounttable.
nhep=$(./mounttable glob $ep nh | grep ^nh | cut -d' ' -f2)
[ -z $nhep ] && FAIL "line $LINENO: no neighborhood server"

# Mount objects and verify the result.
./mounttable mount "${ep}/myself" "$ep" 5m > /dev/null || FAIL "line $LINENO: failed to mount the mounttable on itself"
./mounttable mount "${ep}/google" /www.google.com:80 5m > /dev/null || FAIL "line $LINENO: failed to mount www.google.com"

# <mounttable>.Glob('*')
got=$(./mounttable glob $ep '*' | sed 's/TTL .m..s/TTL XmXXs/' | sort)
want="[${ep}]
google /www.google.com:80 (TTL XmXXs)
myself ${ep} (TTL XmXXs)
nh ${nhep} (TTL XmXXs)"

if [ "$got" != "$want" ]; then
	FAIL "line $LINENO: unexpected output. Got $got, want $want"
fi

# <neighborhood>.Glob('nhname')
got=$(./mounttable glob $nhep $nhname | sed 's/TTL .m..s/TTL XmXXs/' | sort)
want="[${nhep}]
${nhname} ${ep} (TTL XmXXs)"

if [ "$got" != "$want" ]; then
	FAIL "line $LINENO: unexpected output. Got $got, want $want"
fi

PASS
