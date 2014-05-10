#
# TODO(aghassemi) These commands are just for convenience.
# Remove this file and references to it from README before release.
#

PATH:=$(PATH):$(VEYRON_ENV)/cout/node/bin
VEYRON_WSPR= ../../../../bin/wsprd
VEYRON_PROXY= ../../../../bin/proxy
VEYRON_IDENTITY= ../../../../bin/identityd
VEYRON_PROXY_PORT= 5164
VEYRON_PROXY_ADDR= 127.0.0.1:$(VEYRON_PROXY_PORT)
VEYRON_WSPR_PORT= 5165
VEYRON_IDENTITY_PORT= 5163
VEYRON_JS_API= ../../../../../javascript/api

# Builds everything
default: build
build: buildbrowser

# Builds browser version of FortuneJS
buildbrowser:
	(cd $(VEYRON_JS_API) && ./vgrunt build) && \
	mkdir -p ./browser/lib && \
	cp -rf $(VEYRON_JS_API)/dist/*.* ./browser/lib

# Deploys Veyron daemons
daemons:
	@if [[ ! -e $(VEYRON_PROXY) ]]; then \
		echo "Veyron proxy could not be found in $(VEYRON_PROXY). Please build and install veyron2 and services first"; \
		exit 1; \
	fi
	$(VEYRON_IDENTITY) --port=$(VEYRON_IDENTITY_PORT) | \
	$(VEYRON_PROXY) -addr=$(VEYRON_PROXY_ADDR) | \
	$(VEYRON_WSPR) --v=3 -logtostderr=true -vproxy=$(VEYRON_PROXY_ADDR) --port $(VEYRON_WSPR_PORT) &

killdaemons:
	kill `lsof -t -i:$(VEYRON_IDENTITY_PORT)` ; \
	kill `lsof -t -i:$(VEYRON_WSPR_PORT)` ; \
	kill `lsof -t -i:$(VEYRON_PROXY_PORT)`
