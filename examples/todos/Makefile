GO_BIN=../../../../../scripts/build/go
VEYRON_JS_API=../../../../../javascript/api

GRUNT=./node_modules/.bin/grunt
NPM=${VEYRON_ROOT}/environment/cout/node/bin/npm

# TODO(sadovsky): For some reason the first line below fixes 'start' and 'lint'
# but breaks 'buildgo' and 'echopath', while the second does not fix 'start' and
# 'lint'. For now, we require users to already have node in their paths.
#PATH:=${VEYRON_ROOT}/environment/cout/node/bin:${PATH}  # for node
#PATH:=${PATH}:${VEYRON_ROOT}/environment/cout/node/bin  # for node

default: build
build: buildgo buildnode buildbrowser

buildgo:
	${GO_BIN} install {veyron,veyron2}/...

buildnode:
	${NPM} install

buildbrowser:
	(cd ${VEYRON_JS_API} && ./vgrunt build) && \
	mkdir -p ./todos_appd/js && \
	cp -rf ${VEYRON_JS_API}/dist/*.* ./todos_appd/js

start:
	${GRUNT} start

gofmt:
	gofmt -w ./

lint:
	${GRUNT} jshint

echopath:
	@echo ${PATH}
