# TODO(sadovsky): Eliminate separate {build,run,watch}app rules once everything
# is wired together.

export PATH := node_modules/.bin:${VEYRON_ROOT}/environment/cout/node/bin:${PATH}

VEYRON_JS_API := ${VEYRON_ROOT}/veyron/javascript/api
BUNDLE_JS := todos_appd/public/bundle.js

node_modules:
	npm install
	(cd todos_appd && npm install)

buildgo:
	${VEYRON_ROOT}/veyron/scripts/build/go install veyron/examples/todos/... veyron/services/mounttable/mounttabled veyron.io/store/veyron/services/store/stored veyron/tools/findunusedport veyron/tools/identity

buildapp: node_modules
	browserify -d todos_appd/browser/*.js -p [minifyify --map bundle.js.map --output ${BUNDLE_JS}.map] -o ${BUNDLE_JS}

build: buildgo buildapp

run: build
	./run.sh

test:
	./test.sh

runapp: buildapp
	(cd todos_appd && npm start)

watchapp:
	watch -n 1 make buildapp

gofmt:
	gofmt -w .

clean:
	rm -rf node_modules
	rm -rf todos_appd/node_modules
	rm -rf todos_appd/public/bundle.*
	rm -rf todos_appd/third_party/veyron.*

lint: node_modules
	jshint todos_appd/server.js todos_appd/browser/*.js

.PHONY: buildgo buildapp build run runapp watchapp gofmt clean lint
