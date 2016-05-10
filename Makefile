SHA := $(shell git rev-parse --short HEAD)
VERSION := $(shell cat VERSION)
ITTERATION := $(shell date +%s)
OGOPATH := $(shell echo $$GOPATH)
SRCPATH := "mains/"
BUILDPATH := "build/"
LD_FLAGS := -s -w
export GO15VENDOREXPERIMENT=0


#global build vars
GOVERSION := $(shell go version | sed -e 's/ /-/g')

world: sync-all save update

sync-all: oort-cli oort-value oort-group syndicate cfsdvp cfs formic oohhc-filesysd

save:
	godep save -v ./...

update:
	godep update -v ...

clean:
	rm -rf $(BUILDPATH)

build:
	mkdir -p $(BUILDPATH)
	godep go build -i -v --ldflags "$(LD_FLAGS)" -o build/oort-cli github.com/getcfs/cfs-binary-release/mains/oort-cli
	godep go build -i -v --ldflags "$(LD_FLAGS)" -o build/oort-bench github.com/getcfs/cfs-binary-release/mains/oort-bench
	godep go build -i -v -o build/oort-valued --ldflags " $(LD_FLAGS) \
			-X main.ringVersion=$(shell git -C $$GOPATH/src/github.com/gholt/ring rev-parse HEAD) \
			-X main.oortVersion=$(VERSION) \
			-X main.valuestoreVersion=$(shell git -C $$GOPATH/src/github.com/gholt/store rev-parse HEAD) \
			-X main.cmdctrlVersion=$(shell git -C $$GOPATH/src/github.com/pandemicsyn/cmdctrl rev-parse HEAD) \
			-X main.goVersion=$(shell go version | sed -e 's/ /-/g') \
			-X main.buildDate=$(shell date -u +%Y-%m-%d.%H:%M:%S)" github.com/getcfs/cfs-binary-release/mains/oort-valued
	godep go build -i -v -o build/oort-groupd --ldflags " $(LD_FLAGS) \
			-X main.ringVersion=$(shell git -C $$GOPATH/src/github.com/gholt/ring rev-parse HEAD) \
			-X main.oortVersion=$(VERSION) \
			-X main.valuestoreVersion=$(shell git -C $$GOPATH/src/github.com/gholt/store rev-parse HEAD) \
			-X main.cmdctrlVersion=$(shell git -C $$GOPATH/src/github.com/pandemicsyn/cmdctrl rev-parse HEAD) \
			-X main.goVersion=$(shell go version | sed -e 's/ /-/g') \
			-X main.buildDate=$(shell date -u +%Y-%m-%d.%H:%M:%S)" github.com/getcfs/cfs-binary-release/mains/oort-groupd
	godep go build -i -v -o build/synd --ldflags " $(LD_FLAGS) \
			-X main.ringVersion=$(shell git -C $$GOPATH/src/github.com/gholt/ring rev-parse HEAD) \
			-X main.syndVersion=$(VERSION) \
			-X main.goVersion=$(shell go version | sed -e 's/ /-/g') \
			-X main.buildDate=$(shell date -u +%Y-%m-%d.%H:%M:%S)" github.com/getcfs/cfs-binary-release/mains/synd
	godep go build -i -v -o build/syndicate-client --ldflags " $(LD_FLAGS) \
			-X main.ringVersion=$(shell git -C $$GOPATH/src/github.com/gholt/ring rev-parse HEAD) \
			-X main.syndVersion=$(VERSION) \
			-X main.goVersion=$(shell go version | sed -e 's/ /-/g') \
			-X main.buildDate=$(shell date -u +%Y-%m-%d.%H:%M:%S)" github.com/getcfs/cfs-binary-release/mains/syndicate-client
	godep go build -i -v --ldflags "$(LD_FLAGS)" -o build/cfsdvp github.com/getcfs/cfs-binary-release/mains/cfsdvp
	godep go build -i -v --ldflags "$(LD_FLAGS)" -o build/cfs github.com/getcfs/cfs-binary-release/mains/cfs
	godep go build -i -v -o build/formicd --ldflags " $(LD_FLAGS) \
			-X main.formicdVersion=$(VERSION) \
			-X main.goVersion=$(shell go version | sed -e 's/ /-/g') \
			-X main.buildDate=$(shell date -u +%Y-%m-%d.%H:%M:%S)" github.com/getcfs/cfs-binary-release/mains/formicd
	godep go build -i -v --ldflags "$(LD_FLAGS)" -o build/oohhc-filesysd github.com/getcfs/cfs-binary-release/mains/oohhc-filesysd

darwin: export GOOS=darwin
darwin:
	godep go build -i -v --ldflags "$(LD_FLAGS)" -o build/cfs.osx github.com/getcfs/cfs-binary-release/mains/cfs

compact:
	upx -q -1 build/synd
	upx -q -1 build/oort-valued
	upx -q -1 build/oort-groupd
	upx -q -1 build/formicd

install:
	godep go install -v ./...

test:
	godep go test -v ./...

prerelease: install
	ghr -t $(GITHUB_TOKEN) -u $(GITHUB_USER) --replace --prerelease $(VERSION) $(BUILDPATH)

release: install
	ghr -t $(GITHUB_TOKEN) -u $(GITHUB_USER) --replace $(VERSION) $(BUILDPATH)

oort-cli:
	rsync --delete -av $(OGOPATH)/src/github.com/pandemicsyn/oort/oort-cli $(SRCPATH)
	rsync --delete -av $(OGOPATH)/src/github.com/pandemicsyn/oort/oort-bench $(SRCPATH)

oort-value:
	rsync --delete -av $(OGOPATH)/src/github.com/pandemicsyn/oort/oort-valued $(SRCPATH)

oort-group:
	rsync --delete -av $(OGOPATH)/src/github.com/pandemicsyn/oort/oort-groupd $(SRCPATH)

syndicate:
	rsync --delete -av $(OGOPATH)/src/github.com/pandemicsyn/syndicate/synd $(SRCPATH)
	rsync --delete -av $(OGOPATH)/src/github.com/pandemicsyn/syndicate/syndicate-client $(SRCPATH)

cfsdvp:
	rsync --delete -av $(OGOPATH)/src/github.com/creiht/formic/cfsdvp $(SRCPATH)

cfs:
	rsync --delete -av $(OGOPATH)/src/github.com/creiht/formic/cfs $(SRCPATH)

formic:
	rsync -a --delete $(OGOPATH)/src/github.com/creiht/formic/formicd $(SRCPATH)

oohhc-filesysd:
	rsync --delete -av $(OGOPATH)/src/github.com/letterj/oohhc/oohhc-filesysd $(SRCPATH)
