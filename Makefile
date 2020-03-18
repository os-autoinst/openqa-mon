default: all
all: openqa-mon
openqa-mon: openqa-mon.go
	go build openqa-mon.go
install: openqa-mon
	install openqa-mon /usr/local/bin
