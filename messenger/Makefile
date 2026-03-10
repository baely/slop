IMAGE := registry.baileys.dev/messenger

build:
	go build -o messenger .

docker-build:
	docker build --platform linux/amd64 -t $(IMAGE) .

docker-push: docker-build
	docker push $(IMAGE)
