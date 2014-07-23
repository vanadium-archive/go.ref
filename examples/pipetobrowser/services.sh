export PATH=$VEYRON_ROOT/veyron/go/bin:$PATH
export PATH=node_modules/.bin:$PATH

VEYRON_PROXY_ADDR=proxy.envyor.com:8100
VEYRON_WSPR_PORT=7776
HTTP_PORT=8080
NAMESPACE_ROOT=/proxy.envyor.com:8101
VEYRON_IDENTITY_PATH=/tmp/p2b_identity

trap 'kill -TERM 0' SIGINT SIGTERM EXIT

identity generate veyron_p2b_identity > "${VEYRON_IDENTITY_PATH}"

export VEYRON_IDENTITY=$VEYRON_IDENTITY_PATH; \
export NAMESPACE_ROOT=$NAMESPACE_ROOT; \
wsprd --v=1 -alsologtostderr=true -vproxy=$VEYRON_PROXY_ADDR --port $VEYRON_WSPR_PORT & \
serve browser/. --port $HTTP_PORT --compress

wait