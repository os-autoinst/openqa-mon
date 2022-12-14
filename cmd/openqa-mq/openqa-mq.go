package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/streadway/amqp"
)

const VERSION = "1.2"

type Config struct {
	Remote     string   // Remote address
	Keys       []string // Keys or "topics"
	Username   string
	Password   string
	PrintTopic bool // Print topic
	Insecure   bool
	Verbose    bool
}

func (cf *Config) SetOSD() {
	cf.Remote = "amqps://suse:suse@rabbit.suse.de"
	cf.Keys = []string{"suse.openqa.#"}
	cf.Username = "suse"
	cf.Password = "suse"
	cf.Insecure = false
}
func (cf *Config) SetO3() {
	cf.Remote = "amqps://opensuse:opensuse@rabbit.opensuse.org"
	cf.Keys = []string{"opensuse.openqa.#"}
	cf.Username = "opensuse"
	cf.Password = "opensuse"
	cf.Insecure = false
}

var config Config

func printUsage() {
	fmt.Printf("Usage: %s [OPTIONS] [REMOTE] [KEY] [USERNAME] [PASSWORD]\n", os.Args[0])
	fmt.Println("  REMOTE            Define RabbitMQ address (e.g. amqps://opensuse:opensuse@rabbit.opensuse.org)")
	fmt.Println("  KEY               Key to bind to (e.g. opensuse.openqa.# for all openQA messages)")
	fmt.Println("  USERNAME          Username to login to the server")
	fmt.Println("  PASSWORD          Password to login to the server")
	fmt.Println("")
	fmt.Println("Use 'opensuse','o3' or 'ooo' as REMOTE for openqa.opensuse.org, or 'osd' for openqa.suse.de")
	fmt.Println("If remote is a amqp:// or amqps:// URI, username and password will be ignored")
	fmt.Println("OPTIONS")
	fmt.Println("  -r HOST           Set remote endpoint of address. Identical to REMOTE")
	fmt.Println("  -k KEY            Add key to bind to. Multiple parameters are allowed. Identical to KEY")
	fmt.Println("  -u USER           Set username for the amqp connection. Identical to USERNAME")
	fmt.Println("  -p PASS           Set password for the amqpconnection. Identical to PASSWORD")
	fmt.Println("  -i                Use insecure (unencrypted) connection")
	fmt.Println("  -n, --no-topic    Don't print the topic")
	fmt.Println("  -v, --verbose     Verbose mode")
	fmt.Println("  -version          Print program version")
	fmt.Println("")
	fmt.Println("  --osd             Use settings for openqa.suse.de")
	fmt.Println("  --o3,--ooo        Use settings for openqa.opensuse.org (default)")
}

// Returns the remote host from a RabbitMQ URL
func rabbitRemote(remote string) string {
	i := strings.Index(remote, "@")
	if i > 0 {
		return remote[i+1:]
	}
	return remote
}

func parseProgramArguments() error {
	args := os.Args[1:]
	argcount := 0 // Keep counter for distinguishing between remote and key
	keys := make([]string, 0)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		// Parse arguments
		if arg[0] == '-' {
			if arg == "-h" || arg == "--help" {
				printUsage()
				os.Exit(0)
			} else if arg == "--version" {
				fmt.Println("openqa-mq version " + VERSION)
				os.Exit(0)
			} else if arg == "-r" {
				if i >= len(args)-1 {
					return fmt.Errorf("missing host argument")
				}
				i++
				config.Remote = args[i]
			} else if arg == "-k" {
				if i >= len(args)-1 {
					return fmt.Errorf("missing key argument")
				}
				i++
				keys = append(keys, args[i])
			} else if arg == "-u" {
				if i >= len(args)-1 {
					return fmt.Errorf("missing username argument")
				}
				i++
				config.Username = args[i]
			} else if arg == "-p" {
				if i >= len(args)-1 {
					return fmt.Errorf("missing password argument")
				}
				i++
				config.Password = args[i]
			} else if arg == "-i" {
				config.Insecure = true
			} else if arg == "--osd" {
				config.SetOSD()
			} else if arg == "--o3" || arg == "--ooo" {
				config.SetO3()
			} else if arg == "-v" || arg == "--verbose" {
				config.Verbose = true
			} else if arg == "-n" || arg == "--no-topic" || arg == "--notopic" {
				config.PrintTopic = false
			} else {
				return fmt.Errorf("invalid argument: %s", arg)
			}
		} else {
			if argcount == 0 {
				config.Remote = arg
			} else if argcount == 1 {
				keys = append(keys, arg)
			} else if argcount == 2 {
				config.Username = arg
			} else if argcount == 3 {
				config.Password = arg
			} else {
				return fmt.Errorf("too many program arguments")
			}
			argcount++
		}
	}

	// Check for shortcuts
	if config.Remote == "opensuse" || config.Remote == "o3" || config.Remote == "ooo" {
		config.SetO3()
	} else if config.Remote == "suse" || config.Remote == "osd" {
		config.SetOSD()
	}

	// Apply custom keys
	if len(keys) > 0 {
		config.Keys = keys
	}

	return nil
}

// Assemble remote link, if necessary
func assembleRemote() string {
	if strings.Contains(config.Remote, "://") {
		return config.Remote
	} else {
		// Assemble remote
		protocol := "amqps"
		if config.Insecure {
			protocol = "amqp"
		}
		return fmt.Sprintf("%s://%s:%s@%s", protocol, config.Username, config.Password, config.Remote)
	}
}

func main() {
	config.Keys = make([]string, 0)
	config.Verbose = false
	config.PrintTopic = true
	config.SetO3() // Use openqa.opensuse.org by default

	if err := parseProgramArguments(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	remote := assembleRemote()
	if config.Verbose {
		fmt.Printf("Connecting to %s ... \n", config.Remote)
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

	if config.Verbose {
		fmt.Printf("Opening channel ... \n")
	}
	ch, err := con.Channel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Channel error: %s\n", err)
		os.Exit(1)
	}
	defer ch.Close()
	// Produce a new queue and bind it to the desired routing keys. We set the auto-delete flag to avoid spamming the server
	if config.Verbose {
		fmt.Printf("Declare a new exclusive queue ... \n")
	}
	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error declaring queue: %s\n", err)
		os.Exit(1)
	}
	if config.Verbose {
		fmt.Printf("  Queue: '%s' \n", q.Name)
	}
	for _, key := range config.Keys {
		if config.Verbose {
			fmt.Printf("  Binding to key '%s' ... \n", key)
		}
		if err := ch.QueueBind(q.Name, key, "pubsub", false, nil); err != nil {
			fmt.Fprintf(os.Stderr, "Error binding to queue: %s\n", err)
			os.Exit(1)
		}
	}

	// Receive messages from the established channel
	if config.Verbose {
		fmt.Printf("Starting receive ... \n")
	}
	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "recveive error: %s\n", err)
		os.Exit(1)
	}
	go func() {
		for d := range msgs {
			if config.PrintTopic {
				fmt.Printf("%s %s\n", d.RoutingKey, d.Body)
			} else {
				fmt.Printf("%s\n", d.Body)
			}
		}
	}()
	if config.Verbose {
		fmt.Printf("Connection established. Waiting for messages.\n")
	} else {
		fmt.Fprintf(os.Stderr, "RabbitMQ connected: %s\n", rabbitRemote(remote))
	}

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
