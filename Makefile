BIN=dexstats

build: ensure-bin
	@go build -o bin/${BIN} ./...

cross-build: ensure-bin
	@GOOS=linux GOARCH=amd64 go build -o bin/${BIN} ./...

ensure-bin:
	@mkdir -p bin

clean:
	@rm -rf bin

