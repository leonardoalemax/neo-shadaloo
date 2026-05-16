.PHONY: swagger build run

swagger:
	swag init -g main.go --output docs

build:
	go build -o bin/neo-shadaloo .

run:
	go run .
