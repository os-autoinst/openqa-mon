default: all
all: openqa-mon openqa-mq
openqa-mon: cmd/openqa-mon/openqa-mon.go cmd/openqa-mon/terminal.go cmd/openqa-mon/jobs.go cmd/openqa-mon/config.go
	go build -o openqa-mon $^
openqa-mq: cmd/openqa-mq/openqa-mq.go
	go build -o $@ $^

install: openqa-mon
	install openqa-mon /usr/local/bin
	install openqa-mq /usr/local/bin
	install doc/openqa-mon.8 /usr/local/man/man8/
uninstall:
	rm -f /usr/local/bin/openqa-mon
	rm -f /usr/local/bin/openqa-mq
	rm -f /usr/local/man/man8/openqa-mon.8
