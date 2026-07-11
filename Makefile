BINARY := csi-driver-synced-hostpath
BUILD_DIR := $(shell pwd)/build
CHART_NAME := $(shell grep 'name:' deployment/helm-chart/Chart.yaml | awk '{print $$2}')
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.1")
SEMVER := $(shell echo $(VERSION) | sed 's/^v//')
LD_ARGS ?= -ldflags "-X github.com/touchardv/csi-driver-synced-hostpath/internal/driver.VendorVersion=$(VERSION)"
IMAGE := quay.io/touchardv/csi-driver-synced-hostpath
GENERATED_SOURCES := internal/synced/file.pb.go internal/synced/file_grpc.pb.go
GOARCH := $(shell go env GOARCH)
GOOS := $(shell go env GOOS)
HELM_SOURCES := $(shell find deployment/helm-chart -type f 2>/dev/null)
SOURCES := $(shell find . -name '*.go')
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

$(BUILD_DIR)/$(CHART_NAME)-$(SEMVER).tgz: $(HELM_SOURCES)
	helm package deployment/helm-chart -d $(BUILD_DIR) --version $(SEMVER) --app-version $(SEMVER)

$(BINARY)-linux-$(GOARCH): $(BUILD_DIR) $(GENERATED_SOURCES) $(SOURCES)
	go mod tidy
	GOOS=linux GOARCH=$(GOARCH) go build $(LD_ARGS) -o $(BUILD_DIR)/$(BINARY)-linux-$(GOARCH) ./cmd/synced-hostpath

.PHONY: clean
clean:
	rm -f $(GENERATED_SOURCES)
	rm -rf $(BUILD_DIR)
	go clean

.PHONY: install
install: $(BUILD_DIR)/$(CHART_NAME)-$(SEMVER).tgz
	helm upgrade dev-csi-synced-hostpath $(BUILD_DIR)/$(CHART_NAME)-$(SEMVER).tgz --install

internal/synced/file.pb.go: proto/file.proto
	protoc --go_out=internal proto/file.proto

internal/synced/file_grpc.pb.go: proto/file.proto
	protoc --go-grpc_out=internal proto/file.proto

.PHONY: generate-sources
generate-sources: $(GENERATED_SOURCES)

.PHONY: package
package: package-helm-chart package-image

.PHONY: package-helm-chart
package-helm-chart: $(BUILD_DIR)/$(CHART_NAME)-$(SEMVER).tgz

.PHONY: package-image
package-image: $(BINARY)-linux-$(GOARCH)
	docker buildx build --progress plain \
		--platform $(DOCKER_BUILDX_PLATFORM) \
		--tag $(IMAGE):v$(SEMVER) --load -f deployment/Dockerfile .

.PHONY: push-helm-chart
push-helm-chart: $(BUILD_DIR)/$(CHART_NAME)-$(SEMVER).tgz
	helm push $(BUILD_DIR)/$(CHART_NAME)-$(SEMVER).tgz oci://quay.io/touchardv/charts

.PHONY: template
template: $(BUILD_DIR)/$(CHART_NAME)-$(SEMVER).tgz
	helm template $(BUILD_DIR)/$(CHART_NAME)-$(SEMVER).tgz

.PHONY: test
test: $(GENERATED_SOURCES)
	go test -v -cover -timeout 10s ./...
	helm lint deployment/helm-chart

.PHONY: run
run: $(BUILD_DIR)/$(BINARY)
	$(BUILD_DIR)/$(BINARY) -v=4 --nodeid=local --socket-path=/tmp/csi.sock --state-dir=/tmp/state --enable-file-server

.PHONY: run-e2e-test
run-e2e-test:
	deployment/test/run-test.sh

.PHONY: uninstall
uninstall:
	helm uninstall --ignore-not-found dev-csi-synced-hostpath
