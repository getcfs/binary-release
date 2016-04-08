SHA := $(shell git rev-parse --short HEAD)
VERSION := $(shell cat VERSION)
ITTERATION := $(shell date +%s)
OGOPATH := $(shell echo $$GOPATH)
SRCPATH := "mains/"
BUILDPATH := "build/"
export GO15VENDOREXPERIMENT=0


#global build vars
GOVERSION := $(shell go version | sed -e 's/ /-/g')
RINGVERSION := $(shell python -c 'import sys, json; print [x["Rev"] for x in json.load(sys.stdin)["Deps"] if x["ImportPath"] == "github.com/gholt/ring"][0]' < Godeps/Godeps.json)
VERSION := $(shell python -c 'import sys, json; print [x["Rev"] for x in json.load(sys.stdin)["Deps"] if x["ImportPath"] == "github.com/gholt/ring"][0]' < Godeps/Godeps.json)

world: sync-all save update

sync-all: oort-cli oort-value oort-group syndicate cfsdvp cfs cfswrap formic oohhc-acct oohhc-cli oohhc-filesysd

save:
	godep save -v ./...

update:
	godep update -v ./...

clean:
	rm -rf $(BUILDPATH)

build:
	mkdir -p $(BUILDPATH)
	godep go build -i -v -o build/oort-cli github.com/getcfs/cfs-binary-release/mains/oort-cli
	godep go build -i -v -o build/oort-bench github.com/getcfs/cfs-binary-release/mains/oort-bench
	godep go build -i -v -o build/oort-valued github.com/getcfs/cfs-binary-release/mains/oort-valued
	godep go build -i -v -o build/oort-groupd github.com/getcfs/cfs-binary-release/mains/oort-groupd
	godep go build -i -v -o build/synd github.com/getcfs/cfs-binary-release/mains/synd
	godep go build -i -v -o build/syndicate-client github.com/getcfs/cfs-binary-release/mains/syndicate-client
	godep go build -i -v -o build/cfsdvp github.com/getcfs/cfs-binary-release/mains/cfsdvp
	godep go build -i -v -o build/cfs github.com/getcfs/cfs-binary-release/mains/cfs
	godep go build -i -v -o build/formicd github.com/getcfs/cfs-binary-release/mains/formicd
	godep go build -i -v -o build/cfswrap github.com/getcfs/cfs-binary-release/mains/cfswrap
	godep go build -i -v -o build/oohhc-acctd github.com/getcfs/cfs-binary-release/mains/oohhc-acctd
	godep go build -i -v -o build/oohhc-cli github.com/getcfs/cfs-binary-release/mains/oohhc-cli
	godep go build -i -v -o build/oohhc-filesysd github.com/getcfs/cfs-binary-release/mains/oohhc-filesysd

install:
	godep go install -v ./...

test:
	godep go test -v ./...

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

cfswrap:
	cp -av $(OGOPATH)/src/github.com/creiht/formic/cfswrap $(SRCPATH)

formic:
	cp -av $(OGOPATH)/src/github.com/creiht/formic/formicd $(SRCPATH)

oohhc-acct:
	cp -av $(OGOPATH)/src/github.com/letterj/oohhc/oohhc-acctd $(SRCPATH)

oohhc-cli:
	cp -av $(OGOPATH)/src/github.com/letterj/oohhc/oohhc-cli $(SRCPATH)

oohhc-filesysd:
	cp -av $(OGOPATH)/src/github.com/letterj/oohhc/oohhc-filesysd $(SRCPATH)
