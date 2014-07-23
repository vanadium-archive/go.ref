#!/bin/sh

tl=$(git rev-parse --show-toplevel)
go=$tl/scripts/build/go
bin=$tl/go/bin
script=$0
bd=$(dirname $script)
bd=$(realpath $bd)

cleanup() {
	code=$1; msg=$2; shift; shift
	[ "$*" != "" ] && echo "cleanup: $*"
	rm -f $bd/.ep $bd/.id
	kill $pid > /dev/null 2>&1
	killall inspectord > /dev/null 2>&1
	echo $script $msg
	return $code
}

($go install veyron/examples/inspector/... veyron/tools/identity/...) || cleanup 1 FAIL "binary builds failed" || exit 1

# Generate an identity for the client and server
# For now, using the same one for both
$bin/identity generate "inspector" >$bd/.id || cleanup 1 FAIL "server identity" || exit 1
export VEYRON_IDENTITY=$bd/.id

$bin/inspectord >$bd/.ep &
pid=$!

for i in 1 2 3 4; do
	ep=$(cat $bd/.ep)
	if [ "$ep" != "" ]; then
		break
	fi
	sleep $i
done

[ ! $ep ] && cleanup 0 FAIL "no server" && exit 1

$bin/inspector --service /$ep/stubbed/files -glob='m*' || cleanup 1 FAIL "stubbed/files" || exit 1
$bin/inspector --service /$ep/stubless/files -glob='m*'|| cleanup 1 FAIL "stubless/files" || exit 1

cleanup 0 PASS && exit 0
