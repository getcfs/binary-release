SHA := $(shell git rev-parse --short HEAD)
VERSION := $(shell cat VERSION)
ITTERATION := $(shell date +%s)
LOCALPKGS :=  $(shell go list ./... | grep -v /vendor/)

deps:
	go get -u -f $(LOCALPKGS)

build:
	mkdir -p packaging/output
	mkdir -p packaging/root/usr/local/bin
	go build -i -v -o packaging/root/usr/local/bin/cfs github.com/creiht/formic/cfs
	go build -i -v -o packaging/root/usr/local/bin/cfsdvp github.com/creiht/formic/cfsdvp
	go build -i -v -o packaging/root/usr/local/bin/formicd --ldflags " \
		-X main.formicdVersion=$(shell git rev-parse HEAD) \
		-X main.goVersion=$(shell go version | sed -e 's/ /-/g') \
		-X main.buildDate=$(shell date -u +%Y-%m-%d.%H:%M:%S)" github.com/creiht/formic/formicd

darwin: export GOOS=darwin
darwin:
	GOOS=darwin go build -i -v -o packaging/root/usr/local/bin/cfs.osx github.com/creiht/formic/cfs

clean:
	rm -rf packaging/output
	rm -f packaging/root/usr/local/bin

install: build
	cp -av packaging/root/usr/local/bin/* $(GOPATH)/bin

test:
	go test ./...

packages: clean deps build deb
