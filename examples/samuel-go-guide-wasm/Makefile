PLUGIN := plugin.wasm

.PHONY: wasm test clean

wasm:
	tinygo build -o $(PLUGIN) -target=wasi -no-debug -opt=2 ./cmd

test:
	go test ./...

clean:
	rm -f $(PLUGIN)
