# Flink Chaos Operator Makefile

# Tool versions
CONTROLLER_GEN_VERSION ?= v0.14.0
GOLANGCI_LINT_VERSION  ?= v1.57.2

# Binary output directory
BIN_DIR ?= bin

# Local controller-gen binary path
CONTROLLER_GEN ?= $(BIN_DIR)/controller-gen

# Go build settings
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

.PHONY: all
all: generate fmt build

## generate: run controller-gen to produce deepcopy functions and CRD manifests
.PHONY: generate
generate: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		object:headerFile="hack/boilerplate.go.txt" \
		paths="./api/..."
	$(MAKE) manifests

## manifests: generate CRD YAML into config/crd/bases/
.PHONY: manifests
manifests: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		crd:trivialVersions=false \
		rbac:roleName=chaos-operator-role \
		paths="./api/..." \
		output:crd:artifacts:config=config/crd/bases

## fmt: run gofmt over all Go source files
.PHONY: fmt
fmt:
	gofmt -l -w .

## lint: run golangci-lint
.PHONY: lint
lint:
	golangci-lint run ./...

## test: run unit tests with race detector
.PHONY: test
test:
	go test -race -count=1 ./...

## integration-test: run integration tests against envtest
.PHONY: integration-test
integration-test:
	go test -tags=integration -race -count=1 ./internal/controller/...

## e2e-test: run end-to-end tests against a live cluster (requires kind)
.PHONY: e2e-test
e2e-test:
	go test -tags=e2e -count=1 ./test/e2e/...

## test-all: run unit and integration tests
.PHONY: test-all
test-all: test integration-test

## build: compile the controller and CLI binaries
.PHONY: build
build: build-controller build-cli

## build-controller: compile the operator controller binary
.PHONY: build-controller
build-controller:
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -o $(BIN_DIR)/chaos-operator ./cmd/controller

## build-cli: compile the kubectl-fchaos CLI binary
.PHONY: build-cli
build-cli:
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -o $(BIN_DIR)/kubectl-fchaos ./cmd/kubectl-fchaos

## docker-build: build the operator container image
.PHONY: docker-build
docker-build:
	docker build -t chaos-operator:latest .

## docker-push: push the operator container image
.PHONY: docker-push
docker-push:
	docker push chaos-operator:latest

## deploy: deploy the operator to the current Kubernetes cluster context
.PHONY: deploy
deploy: manifests
	kubectl apply -f config/crd/bases/
	kubectl apply -f manifests/

## undeploy: remove the operator from the current Kubernetes cluster context
.PHONY: undeploy
undeploy:
	kubectl delete --ignore-not-found -f manifests/
	kubectl delete --ignore-not-found -f config/crd/bases/

## clean: remove generated binaries
.PHONY: clean
clean:
	rm -rf $(BIN_DIR)

# ---------------------------------------------------------------------------
# Tool installation
# ---------------------------------------------------------------------------

$(CONTROLLER_GEN): $(BIN_DIR)
	GOBIN=$(shell pwd)/$(BIN_DIR) go install \
		sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION)

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

## help: display this help message
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ": "}; {printf "  \033[36m%-30s\033[0m %s\n", $$2, $$3}'
