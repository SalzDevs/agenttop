.PHONY: build run clean test

build:
	go build -o agenttop .

run: build
	./agenttop

clean:
	rm -f agenttop

test:
	go test ./...
