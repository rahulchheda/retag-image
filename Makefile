# Makefile for building retag-image controller
# Reference Guide - https://www.gnu.org/software/make/manual/make.html

IS_DOCKER_INSTALLED = $(shell which docker >> /dev/null 2>&1; echo $$?)

# list only our namespaced directories
PACKAGES = $(shell go list ./... | grep -v '/vendor/')

# docker info
DOCKER_REPO ?= pingtorahulchheda
DOCKER_IMAGE ?= retag-image
DOCKER_TAG ?= 1.0.0-dev

.PHONY: all
all: deps format lint build test dockerops dockerops-amd64

.PHONY: help
help:
	@echo ""
	@echo "Usage:-"
	@echo "\tmake gotasks   -- builds the retag-image controller binary"
	@echo "\tmake dockerops -- builds & pushes the retag-image controller image"
	@echo ""


.PHONY: gotasks
gotasks: format lint build

.PHONY: format
format:
	@echo "------------------"
	@echo "--> Running go fmt"
	@echo "------------------"
	@go fmt $(PACKAGES)

.PHONY: lint
lint:
	@echo "------------------"
	@echo "--> Running golint"
	@echo "------------------"
	@golint $(PACKAGES)
	@echo "------------------"
	@echo "--> Running go vet"
	@echo "------------------"
	@go vet $(PACKAGES)

.PHONY: build
build:
	@echo "------------------"
	@echo "--> Build Retag-Image Controller"
	@echo "------------------"
	@./build/go-multiarch-build.sh github.com/rahulchheda/retag-image/cmd

.PHONY: test
test:
	@echo "------------------"
	@echo "--> Run Go Test"
	@echo "------------------"
	@go test ./... -coverprofile=coverage.txt -covermode=atomic -v

.PHONY: dockerops
dockerops:
	@echo "------------------"
	@echo "--> Build & Push retag-image docker image"
	@echo "------------------"
	docker build --file build/Dockerfile --progress plain --tag $(DOCKER_REPO)/$(DOCKER_IMAGE):$(DOCKER_TAG) .