all: build

build:
	go run bootstrap.go build

dev:
	go run bootstrap.go dev

lint:
	go run bootstrap.go lint

install:
	go run bootstrap.go install
