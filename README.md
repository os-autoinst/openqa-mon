# openqa-mon

CLI monitoring client for [openQA](https://open.qa) written in plain simple go for periodic live monitoring in a terminal (See demo below)

![Demo of openqa-mon in action](demo.gif)

## Build/Run

`openqa-mon` is written in plain go without any additional requirements. Build it with the provided `Makefile`

    $ make
    $ sudo make install     # install the binary to /usr/local/bin
    
    $ openqa-mon http://your-instance.qam.suse.de/

Or simply

    $ go run openqa-mon.go http://your-instance.qam.suse.de/

## Usage

    openqa-mon http://openqa.suse.de/

Running `openqa-mon` against the main internal instance (http://openqa.suse.de/) works, but is slow.

This tool has been designed to monitor the jobs on your own instance.

### Periodical monitoring

**DISCLAIMER** PLEASE DON'T RUN THIS AGAINST ANY PRODUCTIVE INSTANCE (especially not http://openqa.suse.de/). I don't know how much load this puts on those instances!

Running it with your own instance works nicely

    ## Put this in your ~/.bashrc (or whatever shell you are using)
    alias oqa-mon="watch -c -n 1 openqa-mon http://your-instance.qam.suse.de/"

After that you simply run `oqa-mon` and you can continuously monitor the progress of your runs

![Screenshot of openqa-mon in action](Screenshot.png)