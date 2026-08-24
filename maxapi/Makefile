.PHONY: all install build test compare validate clean

all: build

node_modules: package.json
	npm install
	@touch node_modules

install: node_modules

build: node_modules
	npx tsp compile .
	python3 scripts/seal_additional_properties.py
	python3 scripts/fill_inherited_stubs.py

compare: build
	python3 scripts/compare.py

validate: build
	python3 scripts/validate_kb.py -q

test: validate compare

clean:
	rm -rf tsp-output
