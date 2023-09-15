# gominer

gominer is an application for performing Proof-of-Work (PoW) mining on the
Decred network after the activation of
[DCP0011](https://github.com/decred/dcps/blob/master/dcp-0011/dcp-0011.mediawiki)
using BLAKE3.  It supports solo mining using OpenCL and CUDA devices.

[User Reported Hashrates](#user-reported-hashrates)

## Downloading

Binaries are not currently available.  See the [Building](#building)
([Windows](#windows), [Linux](#linux)) section for details on how to build
`gominer` from source.

## Configuring `gominer`

`gominer` needs to acquire work in order to have something to solve.  Currently, the only supported method is solo mining via a `dcrd` RPC server.  There are plans to support [dcrpool](https://github.com/decred/dcrpool) for pooled mining in the future.

In order to communicate with the `dcrd` RPC server, `gominer` must be configured
with `dcrd`'s RPC server credentials.

- Obtain the RPC username and password by finding the `rpcuser` and `rpcpass`
  entries in the `dcrd.conf` file
  - Windows: `%LOCALAPPDATA%\Dcrd\dcrd.conf`
  - Linux: `~/.dcrd/dcrd.conf`
  - MacOs: `~/Library/Application Support/Dcrd/dcrd.conf`
- Create a `gominer.conf` file at the platform-specific path that contains the
  **exact same** `rpcuser=` and `rpcpass=` lines you obtained from the
  `dcrd.conf` file in the previous step
  - Windows: `%LOCALAPPDATA%\Gominer\gominer.conf`
  - Linux: `~/.gominer/gominer.conf`
  - MacOS: `~/Library/Application Support/Gominer/gominer.conf`
  - The `gominer.conf` config file should have at least the following lines:
  ```
  rpcuser=<same rpcuser from dcrd.conf>
  rpcpass=<same rpcpass from dcrd.conf>
  ```

Next, `dcrd` must be configured with a mining address to send the payment for
mined blocks.  That is accomplished by either launching `dcrd` with the
`--miningaddr=Ds...` CLI flag or adding a `miningaddr=Ds...` to the
aforementioned `dcrd.conf` file and restarting `dcrd`.

## Running

### Benchmark mode

`gominer` provides a benchmark mode where no work is submitted in order to test
your setup.

```
./gominer -B
```

### Solo Mining on Mainnet

Ensure you have [configured](#configuring-gominer) `gominer` with `dcrd`'s RPC
credentials as well as `dcrd` with a `miningaddr`.  Once the credentials and
mining address have been configured, simply run gominer to begin mining.

```
./gominer
```

## Status API

There is a built-in status API to report miner information. You can set an
address and port with `--apilisten`. There are configuration examples on
[sample-gominer.conf](sample-gominer.conf). If no port is specified, then it
will listen by default on `3333`.

Example usage:

```sh
$ gominer --apilisten="localhost"
```

Example output:

```sh
$ curl http://localhost:3333/
> {
    "validShares": 0,
    "staleShares": 0,
    "invalidShares": 0,
    "totalShares": 0,
    "sharesPerMinute": 0,
    "started": 1504453881,
    "uptime": 6,
    "devices": [{
        "index": 2,
        "deviceName": "GeForce GT 750M",
        "deviceType": "GPU",
        "hashRate": 110127366.53846154,
        "hashRateFormatted": "110MH/s",
        "fanPercent": 0,
        "temperature": 0,
        "started": 1504453880
    }],
    "pool": {
        "started": 1504453881,
        "uptime": 6
    }
}
```

## Building

### Linux

#### Preliminaries

Gominer works with OpenCL (both AMD and NVIDIA) and CUDA (NVIDIA only).  At the
current time, most users have reported that OpenCL gives them higher hashrates
on NVIDIA.

Once you decide on OpenCL or CUDA, you will need to install the
graphics driver for your GPU as well as the headers for OpenCL or CUDA
depending on your choice.

The exact packages are dependent on the specific Linux distribution, but,
generally speaking, you will need the latest AMDGPU-PRO display drivers for AMD
cards and the latest NVIDIA graphics display drivers for NVIDIA cards.  Then,
depending on whether you will build the OpenCL or CUDA version, the specific set
of toolsets, headers and libraries will have to be installed.

For OpenCL, the packages are typically named something similar to
`mesa-opencl-dev` (for AMD) or `nvidia-opencl-dev` (for NVIDIA).

If you're using OpenCL, it is also recommended to install your distribution's
equivalent of the `clinfo` package if you have any issues to ensure your device
can be detected by OpenCL.  When `clinfo` is unable to detect your device,
`gominer` will not be able to either.

For CUDA, on distributions where it is available via the standard package
manager, the required files are usually found as `nvidia-cuda-toolkit`.  NVIDIA
also provides its own [CUDA Toolkit](https://developer.nvidia.com/cuda-downloads)
downloads.

The following sections provide instructions for building various combinations
of `gominer`:

* [NVIDIA on Ubuntu 23.04](#nvidia-on-ubuntu-2304)
* [AMD and NVIDIA on Debian Bookworm](#debian-bookworm)

#### NVIDIA on Ubuntu 23.04

This section provides instructions for building `gominer` on a computer with an
NVIDIA graphics card running Ubuntu 23.04.  Both OpenCL and CUDA build
instructions are provided.

##### Prerequisites

The following steps are applicable for both OpenCL and CUDA builds of `gominer`:

- Detect the model of your NVIDIA GPU and the recommended driver
  - `ubuntu-drivers devices`
- Install the NVIDIA graphics driver
  - **If you agree with the recommended drivers**
    - `sudo ubuntu-drivers autoinstall`
  - **Alternatively, install a specific driver (for example)**
    - `sudo apt install nvidia-driver-525-server`
- Install the basic development tools `git` and `go`
  - `sudo apt install git golang`
- Reboot to allow the graphics driver to load
  - `sudo reboot`
- Obtain the `gominer` source code
  - `git clone https://github.com/decred/gominer`
- Jump to the appropriate section for either
  [OpenCL](#opencl-on-ubuntu) or [CUDA](#cuda-on-ubuntu) 
  depending on which GPU library you want to build `gominer` for

###### OpenCL on Ubuntu

- Install the OpenCL headers
  - `sudo apt install nvidia-opencl-dev`
- Build `gominer`
  - `cd gominer`
  - `go build -tags opencl`
- Test `gominer` detects your GPU(s)
  - `./gominer -l`
- You may now [configure and run](#configuring-gominer) `gominer`

###### CUDA on Ubuntu

- Install the NVIDIA CUDA Toolkit:
  - `sudo apt install nvidia-cuda-toolkit`
- Build `gominer`:
  - `cd gominer`
  - `go generate -tags cuda .`
- Test `gominer` detects your GPU(s):
  - `./gominer -l`
- You may now [configure and run](#configuring-gominer) `gominer`

#### Debian Bookworm

This section provides instructions for building `gominer` on a computer running
Debian bookworm.  Both OpenCL (using either AMD or NVIDIA graphics cards)
and CUDA (NVIDIA graphics cards only) build instructions are provided.

##### Prerequisites

- Enable the non-free (closed source) repository by using your favorite editor
  to modify `/etc/apt/sources.list` and appending `contrib non-free` to the
  `deb` repository
  - `$EDITOR /etc/apt/sources.list`
    - It should look similar to the following
      ```
      deb http://ftp.us.debian.org/debian bookworm-updates main contrib non-free
      deb http://security.debian.org bookworm-security main contrib non-free
      ```
- Update the Apt package manager with the new sources
  - `sudo apt update`
- Install the basic development tools `git` and `go`:
  - `sudo apt install git golang`
- Obtain the `gominer` source code
  - `git clone https://github.com/decred/gominer`

Proceed to install the appropriate graphics card driver and supporting firmware,
based on the hardware available on the computer:

- For AMD GPUs: Install the AMD graphics driver and supporting firmware
  - `sudo apt install firmware-linux firmware-linux-nonfree libdrm-amdgpu1 xserver-xorg-video-amdgpu`
- For NVIDIA GPUs: Install the NVIDIA graphics driver:
  - `sudo apt install nvidia-driver`
- Restart the computer to ensure the driver is loaded
- Jump to the appropriate section for either
  [OpenCL](#opencl-on-debian) or [CUDA](#cuda-on-debian) 
  depending on which GPU library you want to build `gominer` for


###### OpenCL on Debian

This build mode supports both AMD and NVIDIA graphics cards.

- Install the OpenCL headers, OpenCL Installable Client driver and OpenCL lib
  - `sudo apt install opencl-headers mesa-opencl-icd ocl-icd-libopencl1`
- Help the loader find the OpenCL library by creating a symbolic link to it:
  - `ln -s /usr/lib/x86_64-linux-gnu/libOpenCL.so.1 /usr/lib/libOpenCL.so`
- Build `gominer`
  - `cd gominer`
  - `go build -tags opencl`
- Test `gominer` detects your GPU(s)
  - `./gominer -l`
- You may now [configure and run](#configuring-gominer) `gominer`


###### CUDA on Debian

Note that this requires having an NVIDIA graphics card installed on the
computer.

- Install the NVIDIA CUDA Toolkit:
  - `sudo apt install nvidia-cuda-toolkit`
- Build `gominer`:
  - `cd gominer`
  - `go generate -tags cuda .`
- Test `gominer` detects your GPU(s):
  - `./gominer -l`
- You may now [configure and run](#configuring-gominer) `gominer`

### Windows

#### Windows Preliminaries

Gominer works with OpenCL (both AMD and NVIDIA) and CUDA (NVIDIA only).

At the current time, most users have reported that OpenCL gives them higher
hashrates on NVIDIA.  Additionally, building the CUDA-enabled version of
`gominer` on Windows is a much more involved process.  For these reasons, unless
you really want to run the CUDA version for a specific reason, it is recommended
to use OpenCL.

#### Windows Prerequisites

The following steps are applicable for both OpenCL and CUDA builds of `gominer`:

- Download and install [MSYS2](https://www.msys2.org/)
  - Make sure you uncheck `Run MSYS2 now.`
- Launch the `MSYS2 MINGW64` shell from the start menu
  - NOTE: The `MSYS2` installer will launch the `UCRT64` shell by default if
    you didn't uncheck `Run MSYS2 now` as instructed.  That shell will not work,
    so close it if you forgot to uncheck it in the installer.
- From within the `MSYS2 MINGW64` shell enter the following commands to install
  `gcc`, `git`, `go`, `unzip`:
  - `pacman -S mingw-w64-x86_64-gcc mingw-w64-x86_64-tools mingw-w64-x86_64-go git unzip`
  - `git clone https://github.com/decred/gominer`
- **Close the `MSYS2 MINGW64` shell and relaunch it**
  - NOTE: This is necessary to ensure all of the new environment variables are set properly
- Jump to the appropriate section for either
  [OpenCL](#opencl-prerequisites-on-windows) or [CUDA](#cuda-with-nvidia)
  depending on which GPU library you want to build `gominer` for

##### OpenCL Prerequisites on Windows

The following is needed when performing an OpenCL build:

- Still in the `MSYS2 MINGW64` shell enter the following commands to install 
  the light OpenCL SDK:
  - `wget https://github.com/GPUOpen-LibrariesAndSDKs/OCL-SDK/files/1406216/lightOCLSDK.zip`
  - `unzip -d /c/appsdk lightOCLSDK.zip`
- Jump to the appropriate section for either [OpenCL with AMD](#opencl-with-amd) or
  [OpenCL with NVIDIA](#opencl-with-nvidia) depending on which type of GPU you have

##### OpenCL with AMD

- Change to the library directory C:\appsdk\lib\x86_64
  * `cd /c/appsdk/lib/x86_64`
- Copy and prepare the AMD Display Library (ADL) for linking
  - `cp /c/Windows/SysWOW64/atiadlxx.dll .`
  - `gendef atiadlxx.dll`
  - `dlltool --output-lib libatiadlxx.a --input-def atiadlxx.def`
- Build gominer
  - `cd ~/gominer`
  - `go build -tags opencl`
- Test `gominer` detects your GPU(s)
  - `./gominer -l`
- You may now [configure and run](#configuring-gominer) `gominer`

##### OpenCL with NVIDIA

- Build gominer
  - `cd ~/gominer`
  - `go build -tags opencl`
- Test `gominer` detects your GPU(s)
  - `./gominer -l`
- You may now [configure and run](#configuring-gominer) `gominer`

#### CUDA with NVIDIA

Building the CUDA-enabled `gominer` on a Windows platform is tricky, requires
several GB worth of downloads and while we have made attempts at detecting the
necessary tools and automating the building process, it is not guaranteed to
work, in particular as newer or older versions of the various tools are
installed.

This guide has been tested on a Windows 10 machine, with an NVIDIA graphics card
installed, using Microsoft Visual Studio Community Edition 2022 and NVIDIA CUDA
Toolkit version 12.2.  If the automatic builder for `gominer` does not work on
your system, you many need to [manually setup the various
tools](/docs/cuda-manual-windows-build.md).

After fulfilling the [Windows prerequisites](#windows-prerequisites), follow
the following instructions:

- Download and install the appropriate NVIDIA driver
  - https://www.nvidia.com/download/index.aspx
- Download and install the NVIDIA CUDA Toolkit:
  - https://developer.nvidia.com/cuda-toolkit
- Download and install Microsoft Visual Studio:
  - https://visualstudio.microsoft.com/vs/community/
  - Ensure the "Desktop Development with C++" component will be installed
- Build gominer:
  - `go generate -tags cuda .`
  - The warnings about deprecated symbols are safe to ignore
- Test `gominer` detects your GPU(s):
  - `./gominer -l`
- You may now [configure and run](#configuring-gominer) `gominer`

## User Reported Hashrates

### OpenCL

GPU                    | Hashrate
-----------------------|---------
NVIDIA GTX 1060        | 3.0 Gh/s
AMD RX 580             | 3.7 Gh/s
NVIDIA 1660 Super      | 5.0 Gh/s
AMD Vega 56            | 7.0 Gh/s
NVIDIA RTX 3060 Ti     | 8.7 Gh/s
NVIDIA GTX 3080 Mobile | 9.4 Gh/s
NVIDIA RTX 3070        | 10.1 Gh/s
NVIDIA RTX 2080        | 10.4 Gh/s
NVIDIA Tesla V100      | 13.9 Gh/s
NVIDIA Tesla V100S     | 14.6 Gh/s
NVIDIA RTX 4070        | 14.9 Gh/s
NVIDIA RTX 3080        | 15.2 Gh/s
NVIDIA RTX 3090        | 17.6 Gh/s
AMD 7900 XTX           | 23.8 Gh/s
