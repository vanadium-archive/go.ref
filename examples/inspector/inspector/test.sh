#!/bin/sh

tl=$(git rev-parse --show-toplevel)

go=$tl/scripts/build/go

script=$0

cleanup() {
	code=$1; msg=$2; shift; shift
	[ "$*" != "" ] && echo "cleanup: $*"
	rm -f .ep
	kill $pid > /dev/null 2>&1
	killall inspectord > /dev/null 2>&1
	echo $script $msg
	return $code
}

bd=$(dirname $0)
echo "Build directory:" $bd

(cd $bd; $go build .) || exit 1
(cd $bd/../inspectord; $go build .) || exit 1

(cd $bd/../inspectord; ./inspectord) 2> /dev/null > .ep &
pid=$!

for i in 1 2 3 4; do
	ep=$(cat .ep)
	if [ "$ep" != "" ]; then
		break
	fi
	sleep $i
done

[ ! $ep ] && cleanup 0 FAIL "no server" && exit 1

$bd/inspector --service /$ep/stubbed/files -glob='m*' || cleanup 1 FAIL "stubbed/files" || exit 1
$bd/inspector --service /$ep/stubless/files -glob='m*'|| cleanup 1 FAIL "stubless/files" || exit 1

cleanup 0 PASS && exit 0
