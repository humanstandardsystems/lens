BINARY = lens
VERSION = v0.3.0

build:
	go build -ldflags="-s -w" -o $(BINARY) .

build-all:
	mkdir -p bin
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o bin/lens-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o bin/lens-darwin-amd64 .
	@echo "Built: bin/lens-darwin-arm64, bin/lens-darwin-amd64"

release: build-all
	cd bin && cp lens-darwin-arm64 lens && tar -czf lens-darwin-arm64.tar.gz lens && rm lens
	cd bin && cp lens-darwin-amd64 lens && tar -czf lens-darwin-amd64.tar.gz lens && rm lens
	@echo ""
	@echo "Tarballs ready:"
	@ls -lh bin/lens-darwin-*.tar.gz
	@echo ""
	@echo "SHA256 (paste into Formula/lens.rb):"
	@shasum -a 256 bin/lens-darwin-*.tar.gz

install: build
	mv $(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed to /usr/local/bin/$(BINARY)"

clean:
	rm -f $(BINARY)
	rm -rf bin/

.PHONY: build build-all release install clean
