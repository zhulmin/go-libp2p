all: ping-image.tar

ping-image.tar: PingDockerfile go.mod go.sum cmd/ping/
	cd .. && docker build -t go-libp2p-head -f test-plans/PingDockerfile .
	docker image save -o $@ go-libp2p-head

.PHONY: clean

clean:
	rm ping-image.tar