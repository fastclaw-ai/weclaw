.PHONY: build web dev clean

# Build everything: web frontend + Go binary
build: web
	go build -o weclaw .

# Build web UI
web:
	cd web && npm install && npx next build

# Dev mode
dev:
	air -c .air.toml start

# Clean build artifacts
clean:
	rm -rf weclaw web/out web/.next