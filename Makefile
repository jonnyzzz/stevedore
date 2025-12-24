.PHONY: build test test-integration docker-build fmt

build:
	go build -o stevedore .

test:
	go test ./...

test-integration:
	go test ./tests/integration -v -count=1

docker-build:
	docker build -t stevedore:local .

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
