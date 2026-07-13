BINARY := keep-me-alive

.PHONY: all
all: build vet fmt-check test

.PHONY: build
build:
	go build -o $(BINARY) .

.PHONY: run
run: build
	./$(BINARY)

.PHONY: test
test:
	go test ./...

.PHONY: test-race
test-race:
	go test -race ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: fmt-check
fmt-check:
	@fmt_out="$$(gofmt -l .)"; \
	if [ -n "$$fmt_out" ]; then \
		echo "The following files are not gofmt-formatted:"; \
		echo "$$fmt_out"; \
		exit 1; \
	fi

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: clean
clean:
	rm -f $(BINARY)
	rm -f *.db *.db-shm *.db-wal
