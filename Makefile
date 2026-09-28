.PHONY: lint

default: all

GOARGS=
PREFIX=/usr/local/bin/

all: openqa-mon openqa-mq openqa-revtui
openqa-mon: cmd/openqa-mon/*.go internal/main.go
	go build $(GOARGS) -o $@ cmd/openqa-mon/*.go
openqa-mq: cmd/openqa-mq/*.go internal/main.go
	go build $(GOARGS) -o $@ cmd/openqa-mq/*.go 
openqa-revtui: cmd/openqa-revtui/*.go internal/main.go
	go build $(GOARGS) -o $@ cmd/openqa-revtui/*.go

release: cmd/openqa-mon/*.go cmd/openqa-revtui/*.go cmd/openqa-mq/*.go internal/main.go
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o openqa-revtui-x86_64 cmd/openqa-revtui/*.go
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o openqa-mq-x86_64 cmd/openqa-mq/*.go
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o openqa-mon-x86_64 cmd/openqa-mon/*.go
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o openqa-revtui-aarch64 cmd/openqa-revtui/*.go
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o openqa-mq-aarch64 cmd/openqa-mq/*.go
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o openqa-mon-aarch64 cmd/openqa-mon/*.go

requirements:
	go get ./...

install: openqa-mon openqa-mq openqa-revtui
	install openqa-mon $(PREFIX)
	install openqa-mq $(PREFIX)
	install openqa-revtui $(PREFIX)
	install doc/openqa-{mon,mq,revtui}.8 /usr/local/man/man8/
uninstall:
	rm -f /usr/local/bin/openqa-mon
	rm -f /usr/local/bin/openqa-mq
	rm -f /usr/local/bin/openqa-review
	rm -f /usr/local/man/man8/openqa-mon.8

test:
	go test ./...

lint:
	taplo fmt _review/*.toml

clean:
	go clean
	rm openqa-mon openqa-mq openqa-revtui
