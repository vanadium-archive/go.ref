export SHELL := /bin/bash -euo pipefail

node_modules: package.json
	npm prune
	npm install
	touch node_modules

lint:
	jshint .

.PHONY: lint
