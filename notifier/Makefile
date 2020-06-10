# Binary can be overidden with an env var.
NOTIFY_BINARY ?= notifier

SOURCES := $(shell find ../. -name '*.go')

$(NOTIFY_BINARY): $(SOURCES)
	# Compile for Linux
	GOOS=linux go build -o $(NOTIFY_BINARY)	
