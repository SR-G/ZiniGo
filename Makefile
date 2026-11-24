ROOT_DIR := $(CURDIR)
BUILD_DIR := $(ROOT_DIR)/bin
PACKAGE := .
BINARY := zinigo
DISTRIBUTION_DIR := $(ROOT_DIR)/distribution
VERSION := 0.1.0
VERSION_LABEL := SNAPSHOT
VERSION_NAME :=
CGO_ENABLED=CGO_ENABLED=0
LDFLAGS := -trimpath -ldflags "-s -w -X github.com/SR-G/sul.BuildVersion=$(VERSION) -X github.com/SR-G/sul.BuildVersionLabel=$(VERSION_LABEL) -X github.com/SR-G/sul.BuildVersionName=$(VERSION_NAME)"

.PHONY: all build clean distribution format generate help lint mod run test

all: test build

build: ## Build the CLI binary
	mkdir -p $(BUILD_DIR)
	${CGO_ENABLED} go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) $(PACKAGE)

clean: ## Remove generated binaries
	rm -rf $(BUILD_DIR) ${DISTRIBUTION_DIR} >/dev/null 2>&1

distribution: ## [RELEASE] Build the target archive with the expected binaries
	mkdir -p ${DISTRIBUTION_DIR} $(BUILD_DIR) ${BUILD_DIR}/windows_amd64 ${BUILD_DIR}/linux_amd64 ${BUILD_DIR}/linux_arm64 ${BUILD_DIR}/darwin_amd64 ${BUILD_DIR}/darwin_arm64
	${CGO_ENABLED} GOARCH=amd64 GOOS=windows go build ${LDFLAGS} -o ${BUILD_DIR}/windows_amd64/${BINARY}.exe                ${PACKAGE}
	${CGO_ENABLED} GOARCH=amd64 GOOS=linux   go build ${LDFLAGS} -o ${BUILD_DIR}/linux_amd64/${BINARY} ${LINUX_EXTRA_FLAGS} ${PACKAGE}
	${CGO_ENABLED} GOARCH=arm64 GOOS=linux   go build ${LDFLAGS} -o ${BUILD_DIR}/linux_arm64/${BINARY} ${LINUX_EXTRA_FLAGS} ${PACKAGE}
	${CGO_ENABLED} GOARCH=amd64 GOOS=darwin  go build ${LDFLAGS} -o ${BUILD_DIR}/darwin_amd64/${BINARY} ${PACKAGE}
	${CGO_ENABLED} GOARCH=arm64 GOOS=darwin  go build ${LDFLAGS} -o ${BUILD_DIR}/darwin_arm64/${BINARY} ${PACKAGE}

	cp ${BUILD_DIR}/windows_amd64/${BINARY}.exe ${DISTRIBUTION_DIR}/${BINARY}-windows-amd64
	cp ${BUILD_DIR}/linux_amd64/${BINARY}       ${DISTRIBUTION_DIR}/${BINARY}-linux-amd64
	cp ${BUILD_DIR}/linux_arm64/${BINARY}       ${DISTRIBUTION_DIR}/${BINARY}-linux-arm64
	cp ${BUILD_DIR}/darwin_amd64/${BINARY}      ${DISTRIBUTION_DIR}/${BINARY}-darwin-amd64
	cp ${BUILD_DIR}/darwin_arm64/${BINARY}      ${DISTRIBUTION_DIR}/${BINARY}-darwin-arm64

format: ## Format Go source files
	gofmt -w $$(find . -type f -name '*.go')

generate: ## Generate configuration documentation from argweave tags
	go generate ./...

lint: ## Run go vet
	go vet ./...

mod: ## Tidy Go dependencies
	go mod tidy

run: ## Run the CLI with ARGS, for example make run ARGS='--list'
	go run $(PACKAGE) $(ARGS)

test: ## Run all tests
	go test ./...

help:    ## [HELP] Display commands defined in this makefile
	@sed \
		-e '/^[a-zA-Z0-9_\-]*:.*##/!d' \
		-e 's/:.*##\s*/:/' \
		-e 's/^\(.\+\):\(.*\)/$(shell tput setaf 6)\1$(shell tput sgr0):\2/' \
		$(MAKEFILE_LIST) | sort | column -c2 -t -s :