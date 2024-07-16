[![openqa-mon](https://github.com/os-autoinst/openqa-mon/actions/workflows/openqa-mon.yml/badge.svg)](https://github.com/os-autoinst/openqa-mon/actions/workflows/openqa-mon.yml)

Simple CLI monitoring utilities for [openQA](https://open.qa).
This project now consists of three programs:

* [openqa-mon](#openqa-mon) - live monitoring of openQA jobs
* [openqa-mq](#openqa-mq) - Query [RabbitMQ instance](https://rabbit.opensuse.org/) for updates
* [openqa-revtui](#openqa-revtui) - (Experimental) openQA review dashboard

Those utilities are intended as live monitor tool for your jobs. In contrast to the Browser interface they are smaller, more efficient on the resources and should make your life easier :-)

## Building

`openqa-mon` is written in go with some minimal requirements. The `Makefile` provides rules for installing the requirements and building the binaries.

    make requirements     # manually install requirements
	make
    sudo make install     # install the binaries to /usr/local/bin
    make install ~/bib    # install the binary to bin in your home folder

Static builds

     CGO_ENABLED=0 make -B -j4 GOARGS="-buildmode pie"

	 or

	 make static

# `openqa-mon`

![Demo of openqa-mon in action](doc/demo.gif)

## Usage

    SYNOPSIS:
    openqa-mon [OPTIONS] REMOTE [JOBS]
    
      REMOTE - openQA base URL
      JOBS can be: either a single job id, multiple comma separated job ids or a job id range (MIN..MAX or START+INDEX)
                   See examples below for examples
    OPTIONS
      -c N             Periodic monitoring, refresh every N seconds
      -h, --help       Print help message
      
      -b, --bell       Bell notification on job status change
      -n, --notify     Desktop notification on job status change
      -m,--monitor     Enable all notifications
      --no-bell        Disable bell notifications
      --no-notify      Disable desktop notifications
      -s,--silent      Disable all notifications
      
      -f, --follow     Follow jobs, i.e. replace jobs by their clones if available
      -p, --hierarchy  Show job's children as well (job hierarchy)
      
      --config FILE    Set config file

#### Examples

	# Check the job overview
    openqa-mon http://openqa.opensuse.org
    
	# Check the status of the jobs 100,101 and 199
	openqa-mon http://openqa.opensuse.org -j 100,101,199
	
    # Continuous monitoring certain jobs (e.g. job 401558 and 401782)
    openqa-mon -c 5 http://your-instance.suse.de 401558 401782
	
    # Continuous monitoring job range (e.g. jobs 202-205, i.e. jobs 202,203,204,205)
    openqa-mon -c 5 http://your-instance.suse.de 202..205
    openqa-mon -c 5 http://your-instance.suse.de 202+3
    
    # Continuous monitoring with all notifications and job hierarchy (show children)
    openqa-mon -mfpc 2 http://your-instance.suse.de 413

You can omit the `-j` parameter. Every positive, non-zero `integer` parameter will be considered as `job-id` to be monitored

    openqa-mon http://openqa.opensuse.org 100 101 199

### Periodical monitoring

Support for continuous monitoring is given with the `-c SECONDS` parameter:

    # Refresh every 5 seconds
    openqa-mon -c 5 openqa.opensuse.org

Of course this also includes continuous monitoring for certain jobs

    # Monitor job 1211758, refresh every 5 seconds
    openqa-mon -c 5 openqa.opensuse.org -j 1211758

![Example of continous monitoring](doc/OpenQA-Continous.png)


## Config file

`openqa-mon` reads default configuration from `/etc/openqa/openqa-mon.conf` (global config) or in  `~/.openqa-mon.conf` (user config). Copy and modify the example configuration file [openqa-mon.conf](openqa-mon.conf) to `~/.openqa-mon.conf`

	## openqa-mon config file
	## 
	## this is an example config file for openqa-mon. Modify and place this file in
	## /etc/openqa/openqa-mon.conf (global) or in ~/.openqa-mon.conf (user config)
	## 
	## Have a lot of fun ...
	
	
	## Default remote to use, if nothing is defined
	# DefaultRemote = http://openqa.opensuse.org
	## Enable bell notifications
	# Bell = true
	## Enable desktop notifications
	# Notification = true
	## Follow jobs
	# Follow = true

If you comment out and set `DefaultRemote`, the tool will use this for defined job IDs or for displaying the job overview without specifying `REMOTE` as parameter.

## RabbitMQ

Since version 0.7.0, `openqa-mon` has experimental RabbitMQ support. When monitoring jobs from a host with a configured RabbitMQ server, `openqa-mon` will subscribe to the RabbitMQ and listen for job updates there instead of pulling job updates from the instance itself. This feature is by default disabled, unless activated via `--rabbitmq` or via the `RabbitMQ = true` setting in `~/.openqa-mon.conf`.

For RabbitMQ to work, the RabbitMQ servers need to be configured in one of the following files:

* `/etc/openqa/openqamon-rabbitmq.conf` (recommended for system-wide configurations, e.g. OSD and O3)
* `~/.config/openqa/openqamon-rabbitmq.conf` (recommended for custom configurations, e.g. your own openQA instance)

Alternatively, a custom file can be used using the `--rabbit FILE` program argument.

A RabbitMQ configuration file is a ini-style file with the following syntax:

```ini
[openqa.opensuse.org]
Remote = amqps://rabbit.opensuse.org
Queue = opensuse.openqa
Username = opensuse
Password = opensuse
```

When RabbitMQ is enabled, `openqa-mon` will connect to all configured hosts. If all defined jobs have a corresponding RabbitMQ server, then the continuous monitoring will be paused  to avoid pulling. If at least one job has no corresponding RabbitMQ server configured, then polling will be still enabled.

* * *

# `openqa-mq`

## Usage

    openqa-mq ooo           # Monitor the openSUSE RabbitMQ
    openqa-mq osd           # Monitor the SUSE internal openQA instance

`openqa-mq` connects to the given RabbitMQ server and prints all received messages. It might be useful to grep for status updates of certain jobs or whatever else you want to monitor.

# `openqa-revtui`

## Usage

    openqa-revtui [OPTIONS] [FLAVORS]
    openqa-revtui -c config.toml

`openqa-revtui` is a terminal user interface for helping the user to review the jobs of whole job groups. The typical usage is to run `openqa-revtui` with a predefined configuration toml file. The configuration file defines the remote openQA instance to monitor, job groups and additional query parameters, as well as settings to hide jobs that are not interesting for you (e.g. passing jobs).

![Screenshot of a terminal running openqa-revtui showing four failed jobs in purple and a couple of empty job groups](doc/openqa-revtui.png)

You find a set of example configurations in the [review](_review) subfolder.
