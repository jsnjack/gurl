NAME = gurl
PWD:=$(shell pwd)
BIN:=gurl
VERSION=0.0.0
MONOVA:=$(shell which monova dot 2> /dev/null)

version:
ifdef MONOVA
override VERSION=$(shell monova)
else
	$(info "Install monova (https://github.com/jsnjack/monova) to calculate version")
endif

bin/${BIN}: bin/${BIN}_linux_amd64
	cp bin/${BIN}_linux_amd64 bin/${BIN}

bin/${BIN}_linux_amd64: version *.go httplib/*.go
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-X github.com/jsnjack/${BIN}/cmd.Version=${VERSION}" -o bin/${BIN}_linux_amd64

bin/${BIN}_darwin_amd64: version *.go httplib/*.go
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-X github.com/jsnjack/${BIN}/cmd.Version=${VERSION}" -o bin/${BIN}_darwin_amd64

build: test bin/${BIN} bin/${BIN}_linux_amd64 bin/${BIN}_darwin_amd64

test:
	go test ./...

release: build
	tar --transform='s,_.*,,' --transform='s,bin/,,' -cz -f bin/${BIN}_linux_amd64.tar.gz bin/${BIN}_linux_amd64
	tar --transform='s,_.*,,' --transform='s,bin/,,' -cz -f bin/${BIN}_darwin_amd64.tar.gz bin/${BIN}_darwin_amd64
	grm release jsnjack/${BIN} -f bin/${BIN}_linux_amd64.tar.gz -f bin/${BIN}_darwin_amd64.tar.gz -t "v`monova`"

.PHONY: version release build test
