GO =	go

all:
	${GO} build -v ./...

check: test

test:
	${GO} test -cover ./...
	${GO} vet ./...

.PHONY: all check test
