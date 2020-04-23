default: all
all: openqa-mon
openqa-mon: openqa-mon.go
	go build openqa-mon.go

install: openqa-mon
	install openqa-mon /usr/local/bin
	install openqa-mon.8 /usr/local/man/man8/
uninstall:
	rm -f /usr/local/bin/openqa-mon
	rm -f /usr/local/man/man8/openqa-mon.8
