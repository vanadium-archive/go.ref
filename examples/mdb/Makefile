build:
	${VEYRON_ROOT}/veyron/scripts/build/go install veyron/examples/mdb/... veyron/services/mounttable/mounttabled veyron/services/store/stored veyron/tools/identity

run: build
	./run.sh

test:
	./test.sh

.PHONY: build run test
