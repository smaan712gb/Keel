.PHONY: build test install demo tape clean

BIN := keel
ifeq ($(OS),Windows_NT)
BIN := keel.exe
endif

build:
	go build -o $(BIN) ./cmd/keel

test:
	go test ./...

install:
	go install ./cmd/keel

demo: build
	PATH="$(CURDIR):$$PATH" bash examples/demo/race.sh

tape:
	vhs examples/demo/keel.tape

clean:
	rm -f $(BIN)
	rm -rf examples/demo/keel.gif
