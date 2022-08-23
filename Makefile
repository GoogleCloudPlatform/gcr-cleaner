test:
	@go test \
		-count=1 \
		-race \
		-shuffle=on \
		./...
.PHONY: test
