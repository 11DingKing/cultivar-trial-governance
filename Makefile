.PHONY: test race vet build run measure docker-amd64 docker-arm64

test:
	GOTOOLCHAIN=local go test ./... -count=1

race:
	GOTOOLCHAIN=local go test -race ./... -count=1

vet:
	GOTOOLCHAIN=local go vet ./...

build:
	GOTOOLCHAIN=local go build ./...

run:
	GOTOOLCHAIN=local go run ./cmd/server

measure:
	go run ../.agents/skills/go-base-project-create/scripts/measure_project.go -root .

docker-amd64:
	docker build --platform linux/amd64 -t cultivar-trial-governance:amd64 .

docker-arm64:
	docker build --platform linux/arm64 -t cultivar-trial-governance:arm64 .
