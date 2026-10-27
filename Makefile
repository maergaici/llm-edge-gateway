APP_NAME := cloud-native-go
VERSION := 0.1
IMAGE := $(APP_NAME):$(VERSION)

.PHNOY: build run test docker-build docker-run clean

build:
		CGO_ENABLED=0 GOOS=linux go build -o bin/$(APP_NAME) .

run:
		go run main.go

test:
		go test ./...

docker-build:
		docker build -t $(IMAGE) .

docker-run:
		docker run --rm -p 8080:8080 $(IMAGE)

clean:
		rm -rf bin/

		
