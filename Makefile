.PHONY: build test test-integration docker-build fmt

build:
	go build -o stevedore .

test:
	go test ./...

test-integration:
	python3 -m unittest discover -s tests/integration -p 'test_*.py'

docker-build:
	docker build -t stevedore:local .

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
