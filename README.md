# openqa-mon

CLI monitoring client for [openQA](https://open.qa) written in plain simple go

![Screenshot of openqa-mon in action](Screenshot.png)

## Build/Run

`openqa-mon` is written in plain go without any additional requirements. Build it with the provided `Makefile`

    $ make
    $ sudo make install     # install the binary to /usr/local/bin
    
    $ openqa-mon http://your-instance.qam.suse.de/

Or simply

    $ go run openqa-mon.go http://your-instance.qam.suse.de/

## Usage

    openqa-mon http://openqa.suse.de/

Running `openqa-mon` against the main instance works, but is a bit slow. It is highly recommended to run this utility only against your own instance to monitor your job.

### Periodical monitoring

**DISCLAIMER** As I don't know how much pressure this puts on an openQA instance, please do NOT run this against any production environment (e.g. http://openqa.suse.de/). Running it with your own instance works nicely.

    ## Put this in your ~/.bashrc
    alias oqa-mon="watch -c -n 1 openqa-mon http://your-instance.qam.suse.de/"

After that you simply run `oqa-mon` and you can continuously monitor the progress of your runs

![oqa (bash alias) in action](oqa.png) 