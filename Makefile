SHA := $(shell git rev-parse --short HEAD)
VERSION := $(shell cat VERSION)
ITTERATION := $(shell date +%s)
OGOPATH := $(shell echo $$GOPATH)
SRCPATH := "mains/"
BUILDPATH := "build/"
export GO15VENDOREXPERIMENT=1

#global build vars
GOVERSION := $(shell go version | sed -e 's/ /-/g')

world: sync-all update

sync-all: oort-cli oort-value oort-group syndicate cfsdvp cfs formic

update:
	glide up
	glide install

clean:
	rm -rf $(BUILDPATH)

build:
	mkdir -p $(BUILDPATH)
	go build -i -v -o build/oort-cli github.com/getcfs/cfs-binary-release/mains/oort-cli
	go build -i -v -o build/oort-bench github.com/getcfs/cfs-binary-release/mains/oort-bench
	go build -i -v -o build/oort-valued github.com/getcfs/cfs-binary-release/mains/oort-valued
	go build -i -v -o build/oort-groupd github.com/getcfs/cfs-binary-release/mains/oort-groupd
	go build -i -v -o build/synd github.com/getcfs/cfs-binary-release/mains/synd
	go build -i -v -o build/syndicate-client github.com/getcfs/cfs-binary-release/mains/syndicate-client
	go build -i -v -o build/cfsdvp github.com/getcfs/cfs-binary-release/mains/cfsdvp
	go build -i -v -o build/cfs github.com/getcfs/cfs-binary-release/mains/cfs
	go build -i -v -o build/formicd github.com/getcfs/cfs-binary-release/mains/formicd

install:
	go install $(go list ./... | grep -v /vendor/)

test:
	go test $(go list ./... | grep -v /vendor/)

prerelease: install
	ghr -t $(GITHUB_TOKEN) -u $(GITHUB_USER) --replace --prerelease $(VERSION) $(BUILDPATH)

release: install
	ghr -t $(GITHUB_TOKEN) -u $(GITHUB_USER) --replace $(VERSION) $(BUILDPATH)

oort-cli:
	cp -av $(OGOPATH)/src/github.com/pandemicsyn/oort/oort-cli $(SRCPATH)
	cp -av $(OGOPATH)/src/github.com/pandemicsyn/oort/oort-bench $(SRCPATH)

oort-value:
	cp -av $(OGOPATH)/src/github.com/pandemicsyn/oort/oort-valued $(SRCPATH)

oort-group:
	cp -av $(OGOPATH)/src/github.com/pandemicsyn/oort/oort-groupd $(SRCPATH)

syndicate:
	cp -av $(OGOPATH)/src/github.com/pandemicsyn/syndicate/synd $(SRCPATH)
	cp -av $(OGOPATH)/src/github.com/pandemicsyn/syndicate/syndicate-client $(SRCPATH)

cfsdvp:
	cp -av $(OGOPATH)/src/github.com/creiht/formic/cfsdvp $(SRCPATH)

cfs:
	cp -av $(OGOPATH)/src/github.com/creiht/formic/cfs $(SRCPATH)

formic:
	cp -av $(OGOPATH)/src/github.com/creiht/formic/formicd $(SRCPATH)

