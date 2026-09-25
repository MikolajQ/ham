---
sidebar_position: 1
---

# Syntax and Specification

This document specifies the specification of the "Ham Recipe". The following is the directory structure to be 
followed when creating a "Ham Recipe". Since the software is in **alpha** stage, the syntax and spec may change 
without any notice but we will try to keep backward compatibility as much as possible.


```bash
-- <Ham Recipe Name>
 |
 |-- .git (OPTIONAL VERSION CONTROL)
 |-- (ANY DIREcTORIES OR FILES YOU WANT)
 |
 |-- ham.yml # (REQUIRED)
 -
```

The "Ham Recipe Name" can be any valid directory name, but it is recommended
to use some meaningful name. We recommend using **(LineageOS Device Codename)-(Short Form of OS)-(OS Version)**.

Example, LineageOS 19.1 build for OnePlus 6 device can have the Ham Recipe name as ```enchilada-los19.1```.

## Build Environment

During the build, your recipe will run on an **Ubuntu 24.04 LTS** virtual machine at Hetzner (CPX62: 16 shared vCPU, 32 GB RAM, `/ham-build` on the 640 GB local disk; fallback CCX33: 8 dedicated vCPU, 32 GB RAM, 400 GB volume mounted at `/ham-build`). By default the recipe
will not be run in a docker container but will run directly on the VPS provided by Hetzner. We really don't need
docker since the VM itself sort of acts like a container. **But you may install docker with apt install -y -qq, and 
use docker image of your choice**, this decision is totally upto you.

By default we **install all the dependencies required to build LineageOS or AOSP**, we also install android platform
tools by default, **you don't have to install these, in your recipe.** The host also enables **zram** (zstd, size equal to RAM) so a 32 GB machine can finish current LineageOS links without a larger server.

**ccache is not set up**: every build runs on a fresh server, so the cache would always be cold. A rebuild on a kept server is incremental through `out/`. The server tries `nbg1` first and falls back to `fsn1` and `hel1` when Hetzner has no capacity. After a successful build the server detaches and deletes its volume (if any) and then deletes itself, even with `--keep-server` (that flag only keeps failed builds). A kept failed build deletes itself after 6 hours through the `ham-ttl` systemd timer on the server; `touch /tmp/ham-keep` or `systemctl stop ham-ttl.timer` keeps it longer. A single build step is stopped after 12 hours. zram is set up when the kernel has the module (it is installed from `linux-modules-extra` if needed); without it the build goes on without zram. We also install the **repo** command to the
system itself so no need to install that by yourself. We also install some useful tools and system libs. `python` points at Python 3.

Each build will have the following directory created on the build environment for you to use,

* **/ham-recipe** - This is the copy of your ham recipe directory with all it's contents, whatever files you have in your
recipe directory will be available here. So you can use absolute paths to access those files from the recipe.

* **/ham-build** - This is the working directory for you, and will be cd-ed into when executing your build.

:::info

`python` is already Python 3. Ubuntu 24.04 does not ship Python 2.

:::

Additional Environmental Variable will be set if the ham recipe contains arguments from the user.

## Syntax

Now we will specify the syntax for ```ham.yml``` or ```ham.yaml``` YAML file.

### ```title```

The title of your recipe. HAM displays this on the user's terminal when executed. Have a meaningful title for the
user to understand and confirm what the recipe do.

Example, *"Lineage OS 19.1 (Enchilada) (Signed) without GAPPS"*

```yaml
title: "Lineage OS 19.1 (Enchilada) (Signed)"
```

### ```version```

A string which defines a version, change this to trigger a change. Builds on Hetzner is tracked by the SHA256 hash 
of the ```ham.yml``` file, so to trigger a change, change the version string to something. We recommend using
semver.

Example,

```yaml
version: "0.1.0"
```

### ```args```

This is optional, this holds the array of variables required for the build, these variables will be asked from the
user when they invoke ```ham get```. You may use this to get a secret value such as API Key or string to customize
the build. You may also ask for a file to upload to the build server for to use in build, like Android Certificates
or GPG Keys.

Each **argument needs a ```id```, ```prompt```, and ```type```.** Optionally a **```required```** bool which can
fail the ```ham get``` when the user does not provide the value. This is by default **false.**

Example,

```yaml
args:
  - id: android_certs
    prompt: "Path to Android Certs .zip file Un-Encrypted"
    required: true
    type: file

  - id: github_token
    prompt: "Github Repo Token"
    type: secret

  - id: github_user
    prompt: "Github Username"
    type: value
```

These variables **will be available on the build server, so your recipe can use these.** These variables will be
available as a **environment variable**. The **```id``` of the variable** will be used as the **name of the 
environmental variable**, the value of the **environemtal variable** will be the one given by the user during
the ```ham get``` invocation when the build was initially started.

The **```id```** will be **converted to uppercase** and will be set as a **environmental variable**.

Example,
**```id: android_certs```** will be available as **```ANDROID_CERTS```** environmental variable, **in case if it's a 
file, then the environmental variable will have the path to the uploaded file, which you can move to your working 
directory to use it.**

For example, you may use the variables like this,

```yaml
build:
  - name: Copy Android Certificates
    run: |
      cd /root/
      cp $ANDROID_CERTS certs.zip
      rm -rf .android-certs
      mkdir -p .android-certs
      mv certs.zip .android-certs/certs.zip
      cd .android-certs
      unzip certs.zip

  - name: Using Secrets
    run: echo $GITHUB_TOKEN > ~/gh_token.txt 
```

### ```build```

This is the main list of commands for your build. This will be run after installing deps and setting up the environemnt
for you. Like copying files to the server and setting up the required environmental variables.

Each entry in build **must contain a ```name``` and ```run```**. **```run```** can be a multiline string which can
be a list of linux commands executed line by line.

:::info

`python` is already Python 3. Ubuntu 24.04 does not ship Python 2.

:::

#### ```build.name```

This will be displayed on the user's terminal during the progress tracking, so give a meaningful name.

#### ```build.run```

Can be a string or a multiline string which executes a linux command.

Example,

```yaml
build:
  - name: Making Directory
    run: mkdir lineage || true

  - name: Change Directory
    run: cd lineage

  - name: Echoing File
    run: echo 'Hello World' > test.txt

  - name: Test
    run: echo "$PATH" > env.txt

  - name: Use Args
    run: |
      echo "$TELEGRAM_KEY" > key.txt
      sleep 20
      echo "Something"
      
  - name: Running Lineage OS build
    run: sleep 30

  - name: Signing APKs and Build
    run: sleep 20
```

### ```post_build```

This is a list of linux commands which will be executed after the build is succesfully finished, any error in any
command in the build will not run these set of commands. **This is only run after the build is finished without
any error.** You will be cd-ed into ```/ham-build``` when post build is run.

Example,

```yaml
post_build:
  - /ham-recipe/scripts/upload.sh
  - echo "Finished"
```

Note here that we use **/ham-recipe** which is our copy of the ham recipe we are currently building, the ham recipe 
can have any files like bash scripts to use during the build.

## Examples 

You can look at the [community recipes](https://github.com/ham-community) on how it is done.
