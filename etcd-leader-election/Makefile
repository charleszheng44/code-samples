all: server fetch-etcd

server: main.go
	mkdir -p bin
	go build -o bin/server main.go

fetch-etcd:
	@echo "Fetching etcd"
	@mkdir -p bin
	@curl -L https://github.com/etcd-io/etcd/releases/download/v3.5.12/etcd-v3.5.12-darwin-arm64.zip -o etcd-3.5.12-darwin-arm64.zip 
	@unzip etcd-3.5.12-darwin-arm64.zip 
	@mv etcd-v3.5.12-darwin-arm64/etcd bin/etcd
	@mv etcd-v3.5.12-darwin-arm64/etcdctl bin/etcdctl
	@-rm etcd-3.5.12-darwin-arm64.zip
	@-rm -rf etcd-v3.5.12-darwin-arm64

.PHONY: clean
clean:
	@-rm -rf bin
	@-rm -rf etcd-v3.5.12-darwin-arm64
	@-rm -rf etcd-3.5.12-darwin-arm64.zip
	@-rm -rf default.etcd
