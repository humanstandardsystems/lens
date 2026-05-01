BINARY = lens
VERSION = v1.0.0

build:
	go build -ldflags="-s -w" -o $(BINARY) .

build-all:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o bin/lens-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o bin/lens-darwin-amd64 .
	@echo "Built: bin/lens-darwin-arm64, bin/lens-darwin-amd64"

install: build
	mv $(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed to /usr/local/bin/$(BINARY)"

clean:
	rm -f $(BINARY)
	rm -rf bin/

.PHONY: build build-all install clean
