.PHONY: clean test check build.local build.linux build.osx build.docker build.push

BINARY        ?= pdb-controller
VERSION       ?= $(shell git describe --tags --always --dirty)
IMAGE         ?= mikkeloscar/$(BINARY)
TAG           ?= $(VERSION)
GITHEAD       = $(shell git rev-parse HEAD)
GITURL        = $(shell git config --get remote.origin.url)
GITSTATUS     = $(shell git status --porcelain || echo "no changes")
SOURCES       = $(shell find . -name '*.go')
DOCKERFILE    ?= Dockerfile
GOPKGS        = $(shell go list ./... | grep -v /vendor/)
BUILD_FLAGS   ?= -v
LDFLAGS       ?= -X main.version=$(VERSION) -w -s

default: build.local

clean:
	rm -rf build

test:
	go test -v -race -coverprofile=profile.cov -cover $(GOPKGS)

check:
	golangci-lint run ./...

build.local: build/$(BINARY)
build.linux: build/linux/$(BINARY)
build.osx: build/osx/$(BINARY)
build.linux.amd64: build/linux/amd64/$(BINARY)
build.linux.arm64: build/linux/arm64/$(BINARY)

build/$(BINARY): $(SOURCES)
	go build -o build/$(BINARY) $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" .

build/linux/$(BINARY): $(SOURCES)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(BUILD_FLAGS) -o build/linux/$(BINARY) -ldflags "$(LDFLAGS)" .

build/linux/amd64/$(BINARY): $(SOURCES)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(BUILD_FLAGS) -o build/linux/amd64/$(BINARY) -ldflags "$(LDFLAGS)" .

build/linux/arm64/$(BINARY): $(SOURCES)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(BUILD_FLAGS) -o build/linux/arm64/$(BINARY) -ldflags "$(LDFLAGS)" .

build/osx/$(BINARY): $(SOURCES)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(BUILD_FLAGS) -o build/osx/$(BINARY) -ldflags "$(LDFLAGS)" .

build.docker: build.linux
	docker build --rm -t "$(IMAGE):$(TAG)" -f $(DOCKERFILE) --build-arg TARGETARCH= .

build.docker.multiarch: build.linux.amd64 build.linux.arm64
	docker buildx create --config /etc/cdp-buildkitd.toml --driver-opt network=host --bootstrap --use
	docker buildx build --rm --build-arg BASE_IMAGE=container-registry.zalando.net/library/alpine-3.13:latest -t "$(IMAGE):$(TAG)" -f $(DOCKERFILE) --platform linux/amd64,linux/arm64 --push .


build.push: build.docker
	docker push "$(IMAGE):$(TAG)"
