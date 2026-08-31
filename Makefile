GO =	go

all: build check

build:
	${GO} build -v ./...

check: test

test:
	${GO} test -cover ./...
	${GO} vet ./...

.PHONY: all build check test
