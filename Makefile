BINARY := csi-driver-synced-hostpath
BUILD_DIR := $(shell pwd)/build
CHART_NAME := $(shell grep 'name:' deployment/helm-chart/Chart.yaml | awk '{print $$2}')
CHART_VERSION := $(shell grep 'version:' deployment/helm-chart/Chart.yaml | awk '{print $$2}')
IMAGE := quay.io/touchardv/csi-synced-hostpath-driver
GENERATED_SOURCES := internal/synced/file.pb.go internal/synced/file_grpc.pb.go
GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)
SOURCES := $(shell find . -name '*.go')
TAG := latest
TARGET ?= $(shell uname -m)

ifeq ($(GOARCH), arm64)
 DOCKER_BUILDX_PLATFORM := linux/arm64/v8
else ifeq ($(GOARCH), amd64)
 DOCKER_BUILDX_PLATFORM := linux/amd64
endif

.DEFAULT_GOAL := build
.PHONY: build
build: $(BUILD_DIR)/$(BINARY)

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

$(BUILD_DIR)/$(BINARY): $(BUILD_DIR) $(GENERATED_SOURCES) $(SOURCES)
	go mod tidy
	go build $(LD_ARGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/synced-hostpath

$(BUILD_DIR)/$(CHART_NAME)-$(CHART_VERSION).tgz:
	helm package deployment/helm-chart -d $(BUILD_DIR)

$(BINARY)-linux-$(GOARCH): $(BUILD_DIR) $(GENERATED_SOURCES) $(SOURCES)
	go mod tidy
	GOOS=linux GOARCH=$(GOARCH) go build -o $(BUILD_DIR)/$(BINARY)-linux-$(GOARCH) ./cmd/synced-hostpath

.PHONY: clean
clean:
	rm -f $(GENERATED_SOURCES)
	rm -rf $(BUILD_DIR)
	go clean

.PHONY: install
install: $(BUILD_DIR)/$(CHART_NAME)-$(CHART_VERSION).tgz
	helm upgrade dev-csi-synced-hostpath $(BUILD_DIR)/$(CHART_NAME)-$(CHART_VERSION).tgz --install

internal/synced/file.pb.go: proto/file.proto
	protoc --go_out=internal proto/file.proto

internal/synced/file_grpc.pb.go: proto/file.proto
	protoc --go-grpc_out=internal proto/file.proto

.PHONY: package
package: package-helm-chart package-image

.PHONY: package-helm-chart
package-helm-chart: $(BUILD_DIR)/$(CHART_NAME)-$(CHART_VERSION).tgz

.PHONY: package-image
package-image: $(BINARY)-linux-$(GOARCH)
	docker buildx build --progress plain \
		--platform $(DOCKER_BUILDX_PLATFORM) \
		--tag $(IMAGE):$(TAG) --load -f deployment/Dockerfile .

.PHONY: test
test: $(GENERATED_SOURCES)
	go test -v -cover -timeout 10s ./...
	helm lint deployment/helm-chart

.PHONY: run
run: $(BUILD_DIR)/$(BINARY)
	$(BUILD_DIR)/$(BINARY) -v=4 --nodeid=local --socket-path=/tmp/csi.sock --state-dir=/tmp/state --enable-file-server

.PHONY: uninstall
uninstall:
	helm uninstall --ignore-not-found dev-csi-synced-hostpath
