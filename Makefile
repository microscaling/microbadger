default: test

# Build Docker image
# You can specify TYPE=dev to get builds based on Dockerfile.dev 
build: notifier-build docker_build build_output

# Build and push Docker image and trigger rolling deploy in Kubernetes.
deploy: deploy_checks docker_build docker_push k8s_deploy deploy_output

api: docker_build

inspector: docker_build

size: docker_build

notifier: docker_notifier_build

# Image can be overidden with an env var.
DOCKER_IMAGE ?= quay.io/microscaling/microbadger
BINARY ?= microbadger
NOTIFIER_BINARY ?= notifier/notifier

# Get the latest commit.
GIT_COMMIT = $(strip $(shell git rev-parse --short HEAD))

# Get the version number from the code
CODE_VERSION = $(strip $(shell cat VERSION))

ifndef CODE_VERSION
$(error You need to create a VERSION file to build a release)
endif

# Find out if the working directory is clean
GIT_NOT_CLEAN_CHECK = $(shell git status --porcelain)
ifneq (x$(GIT_NOT_CLEAN_CHECK), x)
DOCKER_TAG_SUFFIX = -dirty
endif

# For production builds the tag matches the release version.
ifeq ($(CLUSTER),production)
DOCKER_TAG = $(CODE_VERSION)
# For dev and staging builds add the commit sha. Mark as dirty for dev builds if the working directory isn't clean
else
DOCKER_TAG = $(CODE_VERSION)-$(GIT_COMMIT)$(DOCKER_TAG_SUFFIX)
endif

# Check if the k8s deployment file matches the release version.
K8S_IMAGE = 'image: $(DOCKER_IMAGE):$(CODE_VERSION)'
GREP_DEPLOY = $(shell grep $(K8S_IMAGE) $(KUBE_DEPLOYMENT))

SOURCES := $(shell find . -name '*.go')

clean: 
	rm $(BINARY)
	rm $(NOTIFIER_BINARY)

test:
	go test $(shell go list ./... | grep -v /vendor/)

get-deps:
	go get -t -v ./...

notifier-build:
	cd notifier && $(MAKE)

$(BINARY): $(SOURCES)
	# Compile for Linux
	GOOS=linux go build -o $(BINARY)	

$(NOTIFIER_BINARY): $(SOURCES)
	cd notifier && $(MAKE)

docker_build: $(BINARY)
	# Build Docker image
ifeq ($(TYPE),dev)  
	docker build \
	-f Dockerfile.dev \
	-t $(DOCKER_IMAGE):$(DOCKER_TAG) .
else
	docker build \
  --build-arg VCS_URL=`git config --get remote.origin.url` \
  --build-arg VCS_REF=$(GIT_COMMIT) \
  --build-arg BUILD_DATE=`date -u +"%Y-%m-%dT%H:%M:%SZ"` \
	-t $(DOCKER_IMAGE):$(DOCKER_TAG) .
endif

docker_notifier_build: $(NOTIFIER_BINARY)
ifeq ($(TYPE),dev)  
	docker build \
	-f Dockerfile.dev \
	-t $(DOCKER_IMAGE):$(DOCKER_TAG) .
else
	docker build \
  --build-arg VCS_URL=`git config --get remote.origin.url` \
  --build-arg VCS_REF=$(GIT_COMMIT) \
  --build-arg BUILD_DATE=`date -u +"%Y-%m-%dT%H:%M:%SZ"` \
	-t $(DOCKER_IMAGE):$(DOCKER_TAG) .
endif 

deploy_checks:

ifeq ($(MAKECMDGOALS),deploy)

ifndef CODE_VERSION
$(error You need to create a VERSION file to build a release)
endif

# Don't deploy unless this is a clean repo.
ifneq (x$(GIT_NOT_CLEAN_CHECK), x)
$(error echo You are trying to deploy a build based on a dirty repo)
endif

# Production deploys have extra checks.
ifeq ($(CLUSTER),production)
# See what commit is tagged to match the version
VERSION_COMMIT = $(strip $(shell git rev-list $(CODE_VERSION) -n 1 | cut -c1-7))
ifneq ($(VERSION_COMMIT), $(GIT_COMMIT))
$(error echo You are trying to push a build based on commit $(GIT_COMMIT) but the tagged release version is $(VERSION_COMMIT))
endif

endif

endif

docker_push:
	# Push to DockerHub
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)

build_output:
	@echo Docker Image: $(DOCKER_IMAGE):$(DOCKER_TAG)
