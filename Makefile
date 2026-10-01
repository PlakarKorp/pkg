GO =	go
BIN_DIR ?= bin

all: build check

build:
	${GO} build -v ./...

check: test

validator:
	${GO} build -o ${BIN_DIR}/validator ./cmd/validator

clean:
	rm -rf ${BIN_DIR}

test:
	${GO} test -cover ./...
	${GO} vet ./...

.PHONY: all build check test validator clean
