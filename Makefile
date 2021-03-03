default: all

GOARGS=
PREFIX=/usr/local/bin/

all: openqa-mon openqa-mq openqa-revtui
openqa-mon: cmd/openqa-mon/openqa-mon.go cmd/openqa-mon/config.go cmd/openqa-mon/tui.go cmd/openqa-mon/util.go
	go build $(GOARGS) -o $@ $^
openqa-mq: cmd/openqa-mq/openqa-mq.go
	go build $(GOARGS) -o $@ $^
openqa-revtui: cmd/openqa-revtui/openqa-revtui.go cmd/openqa-revtui/tui.go
	go build $(GOARGS) -o $@ $^

requirements:
	go get github.com/BurntSushi/toml
	go get github.com/grisu48/gopenqa
	go get github.com/streadway/amqp

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
