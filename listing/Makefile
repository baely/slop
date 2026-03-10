.PHONY: build docker-build docker-push

BINARY_NAME=listing
IMAGE_NAME=registry.baileys.dev/listing

build:
	go build -o $(BINARY_NAME) ./cmd/listing

docker-build:
	docker build --platform linux/amd64 -t $(IMAGE_NAME) .

docker-push: docker-build
	docker push $(IMAGE_NAME)
