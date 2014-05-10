todos
=====
export VEYRON_ROOT=/usr/local/google/home/sadovsky/veyron
export PATH=./node_modules/.bin:${VEYRON_ROOT}/environment/cout/node/bin:${PATH}

make build
make start
make lint

vgo install {veyron,veyron2}/...
npm install
gofmt -w v/src/veyron/examples/todos
grunt start
