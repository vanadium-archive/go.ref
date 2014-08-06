VEYRON_JS_API := ${VEYRON_ROOT}/veyron/javascript/api

export PATH := node_modules/.bin:${PATH}

build: buildgo buildnode buildbrowser

buildgo:
	${VEYRON_ROOT}/veyron/scripts/build/go install {veyron,veyron2}/...

buildnode:
	npm install

buildbrowser:
	(cd ${VEYRON_JS_API} && ./vgrunt build) && \
	mkdir -p todos_appd/public/js && \
	cp -rf ${VEYRON_JS_API}/dist/veyron.* todos_appd/public/js

run: build
	./run.sh

# TODO(sadovsky): Merge into other rules once everything's wired up.
runapp:
	(cd todos_appd && npm install && npm start)

gofmt:
	gofmt -w .

clean:
	rm -rf node_modules
	rm -rf todos_appd/node_modules
	rm -rf todos_appd/public/js/veyron.*

lint:
	npm install --dev
	jshint todos_appd/server.js todos_appd/public/js/*.js --exclude "**/veyron.*"

.PHONY: build buildgo buildnode buildbrowser run runapp gofmt clean lint
