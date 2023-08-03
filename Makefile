default: all
static: openqa-mon-static openqa-mq-static openqa-revtui-static

GOARGS=
PREFIX=/usr/local/bin/

all: openqa-mon openqa-mq openqa-revtui
openqa-mon: cmd/openqa-mon/openqa-mon.go cmd/openqa-mon/config.go cmd/openqa-mon/tui.go cmd/openqa-mon/util.go
	go build $(GOARGS) -o $@ $^
openqa-mon-static: cmd/openqa-mon/openqa-mon.go cmd/openqa-mon/config.go cmd/openqa-mon/tui.go cmd/openqa-mon/util.go
	CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o openqa-mon $^
openqa-mq: cmd/openqa-mq/openqa-mq.go
	go build $(GOARGS) -o $@ $^
openqa-mq-static: cmd/openqa-mq/openqa-mq.go
	CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o openqa-mq $^
openqa-revtui: cmd/openqa-revtui/openqa-revtui.go cmd/openqa-revtui/tui.go cmd/openqa-revtui/utils.go
	go build $(GOARGS) -o $@ $^
openqa-revtui-static: cmd/openqa-revtui/openqa-revtui.go cmd/openqa-revtui/tui.go cmd/openqa-revtui/utils.go
	CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o openqa-revtui $^

requirements:
	go get ./...

install: openqa-mon openqa-mq openqa-revtui
	install openqa-mon $(PREFIX)
	install openqa-mq $(PREFIX)
	install openqa-revtui $(PREFIX)
	install doc/openqa-mon.8 /usr/local/man/man8/
uninstall:
	rm -f /usr/local/bin/openqa-mon
	rm -f /usr/local/bin/openqa-mq
	rm -f /usr/local/bin/openqa-review
	rm -f /usr/local/man/man8/openqa-mon.8
