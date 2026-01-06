PROJECT := "hub"
VERSION := `git describe --tags --abbrev=0 2>/dev/null || echo dev`
COMMIT := `git rev-parse --short HEAD`
LDFLAGS := "-s -w" \
  + " -X main.version=" + VERSION \
  + " -X main.commit=" + COMMIT

# show help
default:
	@echo "===== Just help for {{PROJECT}} ====="
	@just --list

# initialize go module
init:
	@echo "===== Init go module for {{PROJECT}} ====="
	go mod init github.com/psvmcc/{{PROJECT}}
	brew install golangci-lint

# clean the build directory
clean:
	rm -rf ./build
	mkdir ./build

# ensure dependencies are up to date
deps:
	@echo "===== Check deps for {{PROJECT}} ====="
	go mod tidy
	go mod vendor

# run tests, checks and linters
check: deps
	@echo "===== Check {{PROJECT}} ====="
	go vet -mod vendor ./...
	go tool staticcheck ./...
	go tool shadow ./...
	go tool govulncheck ./...

# run linter
lint:
	@echo "===== Lint {{PROJECT}} ====="
	golangci-lint run --timeout 5m

# run the application
run: check lint
	@echo "===== Run {{PROJECT}} ====="
	go run main.go s

# build the application
build: clean
	@echo "===== Build {{PROJECT}} ====="
	CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="{{LDFLAGS}}" -o build/{{PROJECT}} main.go

# build the application for Linux / amd64
linux: clean
	@echo "===== Build {{PROJECT}} for Linux / amd64 ====="
	CGO_ENABLED=0 GOOS="linux" GOARCH="amd64" go build -mod=vendor -trimpath -ldflags="{{LDFLAGS}}" -o build/{{PROJECT}}.linux main.go

# build container image
container:
	@echo "===== Build OCI for {{PROJECT}} ====="
	podman build -t psvmcc/{{PROJECT}} .
