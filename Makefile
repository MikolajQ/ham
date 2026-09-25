LDFLAGS="-s -w -X main.AppVersion=1-alpha -X main.GitCommit=$(GIT_COMMIT)"

dev:
	go build cmd/ham/ham.go
	go build cmd/ham-build/ham-build.go

all:
	mkdir -p release
	go mod tidy
	go mod verify
	GOARCH="amd64" \
	GOHOSTARCH="amd64" \
	GOHOSTOS="linux" \
	GOOS="linux" \
	go build -o release/ham-linux-amd64 -ldflags ${LDFLAGS} cmd/ham/ham.go
	GOARCH="386" \
	GOHOSTARCH="amd64" \
	GOHOSTOS="linux" \
	GOOS="linux" \
	go build -o release/ham-linux-i386 -ldflags ${LDFLAGS} cmd/ham/ham.go
	GOARCH="arm64" \
	GOHOSTARCH="amd64" \
	GOHOSTOS="linux" \
	GOOS="linux" \
	go build -o release/ham-linux-arm64 -ldflags ${LDFLAGS} cmd/ham/ham.go
	CC=$(NDK_ROOT)/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang \
        CXX=$(NDK_ROOT)/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang \
	CGO_ENABLED=1 \
	GOARCH="arm64" \
	GOHOSTARCH="amd64" \
	GOHOSTOS="linux" \
	GOOS="android" \
	go build -o release/ham-android-arm64 -ldflags ${LDFLAGS} cmd/ham/ham.go
	GOARCH="amd64" \
	GOHOSTARCH="amd64" \
	GOHOSTOS="linux" \
	GOOS="windows" \
	go build -o release/ham-windows-amd64.exe -ldflags ${LDFLAGS} cmd/ham/ham.go
	GOARCH="amd64" \
	GOHOSTARCH="amd64" \
	GOHOSTOS="linux" \
	GOOS="darwin" \
	go build -o release/ham-macos-amd64 -ldflags ${LDFLAGS} cmd/ham/ham.go
	GOARCH="arm64" \
	GOHOSTARCH="amd64" \
	GOHOSTOS="linux" \
	GOOS="darwin" \
	go build -o release/ham-macos-arm64 -ldflags ${LDFLAGS} cmd/ham/ham.go
	GOARCH="amd64" \
	GOHOSTARCH="amd64" \
	GOHOSTOS="linux" \
	GOOS="linux" \
	go build -o release/ham-build-linux-amd64 -ldflags ${LDFLAGS} cmd/ham-build/ham-build.go

# Client and server binary for this machine, from the same commit.
# `ham get` uploads the ham-build lying next to ham and refuses a
# server binary whose commit differs, so always install both.
LOCAL_COMMIT = $(shell git rev-parse --short HEAD)$(shell git diff --quiet HEAD || echo -dirty)
LOCAL_LDFLAGS = "-s -w -X main.AppVersion=1-alpha-noble -X main.GitCommit=$(LOCAL_COMMIT)"
LOCAL_DIR ?= $(HOME)/bin/rhode

local:
	mkdir -p release
	GOOS=linux GOARCH=amd64 go build -o release/ham -ldflags $(LOCAL_LDFLAGS) cmd/ham/ham.go
	GOOS=linux GOARCH=amd64 go build -o release/ham-build -ldflags $(LOCAL_LDFLAGS) cmd/ham-build/ham-build.go
	install -m 755 release/ham release/ham-build $(LOCAL_DIR)/

clean:
	rm -rf release

