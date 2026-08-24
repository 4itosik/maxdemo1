.PHONY: all install build test compare clean

all: build

node_modules: package.json
	npm install
	@touch node_modules

install: node_modules

build: node_modules
	npx tsp compile .
	python3 scripts/seal_additional_properties.py

compare: build
	python3 scripts/compare.py

test: compare

clean:
	rm -rf tsp-output
