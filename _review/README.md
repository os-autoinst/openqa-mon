# `openqa-revtui` configurations

This directory contains example configuration files for `openqa-revtui`

* [opensuse-microos.toml](opensuse-microos.toml) - Review template for openSUSE MicroOS test runs
* [opensuse-tumbleweed.toml](opensuse-tumbleweed.toml) - Review template for openSUSE Tumbleweed test runs

## SLES templates for QE-C

Find a selection of templates for the qe-c squad (containers and images)

**Recommended review configuration file**: [qe-Maintenance-QEC.toml](qe-Maintenance-QEC.toml) (contains all maintenance test runs except *LATEST*)

Individual files:

* [qec-publiccloud.toml](qec-publiccloud.toml) - Review template for SLES PublicCloud test runs
* [qec-containers.toml](qec-containers.toml) - Review template for SLES container test runs (excluding BCI)
* [qec-bci.toml](qec-bci.toml) - Review template for SLE BCI container images
* [qec-jeos.toml](qec-jeos.toml) - Review template for SLE JeOS images
* [qec-sle-micro.toml](qec-sle-micro.toml) - Review template for SLE Micro
* [qec-sle-wsl.toml](qec-sle-wsl.toml) - Review template for SLE WSL images
* [qec-wicked.toml](qec-wicked.toml) - Review template for wicked images
* [qec-alp.toml](qec-alp.toml) - Review template for ALP (Dolomite)

## Create a TODO template

A `TODO` configuration is a `toml` configuration file, which shows only test run that require an action from a reviewer.
In practice this means it shows only failed and incomplete jobs and hides all currently running, scheduled or passing and softfailing jobs.

To create such a `TODO` configuration, the following template might be useful.

```toml
Instance = "https://openqa.opensuse.org"
RabbitMQ = "amqps://opensuse:opensuse@rabbit.opensuse.org"
RabbitMQTopic = "opensuse.openqa.job.done"
HideStatus = [ "scheduled", "assigned", "passed", "softfailed", "cancelled", "skipped", "running", "reviewed", "user_cancelled"]
RefreshInterval = 60
MaxJobs = 20
GroupBy = "groups"
DefaultParams = { distri="opensuse", version = "Tumbleweed" }
```

The important parameter is the `HideStatus` one. Here we hide all job states, except failures.

The `reviewed` status in HideStatus is a special status, which indicates all failures which have a bugref (bugzilla or progress.opensuse.org issue) in at least one of the comments. Those are considered as "reviewed" jobs.

### Template for SLES

For usage on OSD, replace the `Instance` and `RabbitMQ` variables accordingly. Also update the required query parameters (`DefaultParams`) to match your distri/flavors.

```toml
Instance = "https://openqa.suse.de"
RabbitMQ = "amqps://suse:suse@rabbit.suse.de"
RabbitMQTopic = "suse.openqa.job.done"
DefaultParams = { distri = "sle" }
```
