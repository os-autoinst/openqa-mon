# openqa-mon

CLI monitoring client for OpenQA written in simple go

    # Compile and install to /usr/local/bin
    make
    sudo make install

![Screenshot of openqa-mon in action](Screenshot.png)

## Run

    openqa-mon URL
      URL is the openqa instance URL, e.g. openqa-mon http://your-instance.qam.suse.de/

Running this agains the main instance takes forever and is **NOT** recommended. Please use your own instance

### Periodically monitoring

I put the following in my `~/.bashrc` for continuous monitoring

    # Replace http://phoenix-openqa.qam.suse.de/ with out instance
    alias oqa-mon="watch -c -n 1 openqa-mon http://your-instance.qam.suse.de/"
