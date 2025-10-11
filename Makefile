.PHONY: all build clean test help

all: help

aimux: ./*.mod ./pkg/aimux/*.go ./cmd/aimux/*.go
	go build -o aimux ./cmd/aimux

build: aimux

clean:
	rm -f ./aimux

test: build
	go test ./...

help: build
	./aimux -h
