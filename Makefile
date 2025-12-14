.PHONY: build test test-integration docker-build fmt

build:
\tgo build -o stevedore .

test:
\tgo test ./...

test-integration:
\tpython3 -m unittest discover -s tests/integration -p 'test_*.py'

docker-build:
\tdocker build -t stevedore:local .

fmt:
\tgofmt -w $$(find . -name '*.go' -not -path './vendor/*')
