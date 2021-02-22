package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/streadway/amqp"
)

func printUsage() {
	fmt.Printf("Usage: %s [REMOTE] [KEY]\n", os.Args[0])
	fmt.Println("  REMOTE - RabbitMQ address (e.g. amqps://opensuse:opensuse@rabbit.opensuse.org)")
	fmt.Println("  KEY - Queue key to bind to (e.g. opensuse.openqa.# for all openQA messages)")
	fmt.Println("")
	fmt.Println("Use 'opensuse','o3' or 'ooo' as REMOTE declaration for openSUSE, or 'osd' for the internal openQA instance")
}

// Returns the remote host from a RabbitMQ URL
func rabbitRemote(remote string) string {
	i := strings.Index(remote, "@")
	if i > 0 {
		return remote[i+1:]
	}
	return remote
}

func main() {
	// Default: openSUSE RabbitMQ
	remote := "amqps://opensuse:opensuse@rabbit.opensuse.org"
	key := "opensuse.openqa.#"

	if len(os.Args) > 1 {
		remote = os.Args[1]

		if remote == "-h" || remote == "--help" {
			printUsage()
			os.Exit(0)
		}

		// Provide some nice shortcuts
		if remote == "opensuse" || remote == "o3" || remote == "ooo" {
			fmt.Fprintf(os.Stderr, "Using openSUSE RabbitMQ - See https://rabbit.opensuse.org/\n")
			remote = "amqps://opensuse:opensuse@rabbit.opensuse.org"
			key = "opensuse.openqa.#"
		} else if remote == "suse" || remote == "osd" {
			fmt.Fprintf(os.Stderr, "Using SUSE RabbitMQ - See https://rabbit.suse.de/\n")
			remote = "amqps://suse:suse@rabbit.suse.de"
			key = "suse.openqa.#"
		}
	}
	if len(os.Args) > 2 {
		key = os.Args[2]
	}

	// Establish connection to amqp
	con, err := amqp.Dial(remote)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connection error: %s\n", err)
		os.Exit(1)
	}
	defer con.Close()

	// Establish channel and connect to queue.
	/*
	 * The most important parameter is "key", which defines the topic we are listening to.
	 * The topic format is described as
	   SCOPE.APPLICATION.OBJECT.ACTION
	   ^     ^           ^      ^
	   |     |           |      |
	   |     |           |      +----- What happend with the object (verb in nonfinite form)
	   |     |           +------------ What object was touched by the action
	   |     +------------------------ In which application did the event occur
	   +------------------------------ Was it an internal or external application

	   The topic for openQA related messages on openqa.opensuse.org is e.g. 'suse.openqa.#'
	   Wildcards: '*' stands for one arbitrary word, while '#' stands for multiple arbitrary words
	*/
	ch, err := con.Channel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Channel error: %s\n", err)
		os.Exit(1)
	}
	defer ch.Close()
	q, err := ch.QueueDeclare("", false, false, false, false, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error declaring queue: %s\n", err)
		os.Exit(1)
	}
	if err := ch.QueueBind(q.Name, key, "pubsub", false, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error binding to queue: %s\n", err)
		os.Exit(1)
	}

	// Receive messages from the established channel
	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	go func() {
		for d := range msgs {
			fmt.Printf("%s %s\n", d.RoutingKey, d.Body)
		}
	}()
	fmt.Fprintf(os.Stderr, "RabbitMQ connected: %s\n", rabbitRemote(remote))

	// Terminate on termination signal
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		fmt.Fprintf(os.Stderr, "%s\n", sig)
		done <- true
	}()
	<-done
	os.Exit(1)
}
