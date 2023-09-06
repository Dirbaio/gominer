# gominer

gominer is an application for performing Proof-of-Work (PoW) mining on the
Decred network after the activation of DCP0011 using BLAKE3.  It supports solo
and stratum/pool mining using OpenCL devices.

## Downloading

Linux and Windows 64-bit binaries may be downloaded from:

[https://github.com/decred/decred-binaries/releases/latest](https://github.com/decred/decred-binaries/releases/latest)

## Running

Benchmark mode:

```
gominer -B
```

Solo mining on mainnet using dcrd running on the local host:

```
gominer -u myusername -P hunter2
```

Stratum/pool mining:

```
gominer -o stratum+tcp://pool:port -m username -n password
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

Gominer works with both OpenCL (both AMD and NVIDIA) and CUDA (NVIDIA only).
At the current time, most users have reported that OpenCL gives them higher
hashrates on NVIDIA.

**NOTE: Although gominer works with CUDA, there are not any build instructions
yet.  The will be provided at a later date**.

Once you decide on OpenCL or CUDA, you will need to install the
graphics driver for your GPU as well as the headers for OpenCL or CUDA
depending on your choice.

The exact packages are dependent on the specific Linux distribution, but,
generally speaking, you will need the latest AMDGPU-PRO display drivers for
AMD cards and the latest NVIDIA graphics display drivers for NVIDIA cards.

You will also need the OpenCL headers which is typically named something
similar to `mesa-opencl-dev` (for AMD) or `nvidia-opencl-dev` for NVIDIA.

If you're using OpenCL, it is also recommended to install your distribution's
equivalent of the `clinfo` package if you have any issues to ensure your
device can be detected by OpenCL.

The following sections provide instructions for the following combinations:

* OpenCL for NVIDIA on Ubuntu 23.04
* OpenCL for AMD on Debian Bookworm

#### OpenCL Build Instructions (Works with Both NVIDIA and AMD)

##### OpenCL with NVIDIA on Ubuntu 23.04

- Detect the model of your NVIDIA GPU and the recommended driver
  - `ubuntu-drivers devices`
- Install the NVIDIA graphics driver
  - **If you agree with the recommended drivers**
    - `sudo ubuntu-drivers autoinstall`
  - **Alternatively, install a specific driver (forr example)**
    - `sudo apt install nvidia-driver-525-server`
- Reboot to allow the graphics driver to load
  - `sudo reboot`
- Install the OpenCL headers, `git` adnd `go`
  - `sudo apt install nvidia-opencl-dev git golang`
- Obtain the `gominer` source code
  - `git clone https://github.com/decred/gominer`
- Build `gominer`
  - `cd gominer`
  - `go build -tags opencl`
- Test `gominer` detects your GPU
  - `./gominer -l`

##### OpenCL with AMD on Debian Bookworm

- Enable the non-free (closed source) repository by using your favorite editor
  to modify `/etc/apt/sources.list` and appending `contrib non-free` to the
  `deb` respoitory
  - `$EDITOR /etc/apt/sources.list``
    - It should look similar to the following
      ```
      deb http://ftp.us.debian.org/debian bookworm-updates main contrib non-free
      deb http://security.debian.org bookworm-security main contrib non-free
      ```
- Update the Apt package manager with the new sources
  - `apt update`
- Install the AMD graphics driver and supporting firmware
  - `apt install firmware-linux firmware-linux-nonfree libdrm-amdgpu1 xserver-xorg-video-amdgpu`
- Install the OpenCL headers, `git` adnd `go`
  - `sudo apt install mesa-opencl-dev git golang`
- Obtain the `gominer` source code
  - `git clone https://github.com/decred/gominer`
- Build `gominer`
  - `cd gominer`
  - `go build -tags opencl`
- Test `gominer` detects your GPU
  - `./gominer -l`

#### CUDA Build Instructions (NVIDIA only)

**Build instructions are not available yet.  They will be provided at a later
date**.

### Windows

#### OpenCL Build Instructions (Works with Both NVIDIA and AMD)

##### OpenCL Pre-Requisites

- Download and install [MSYS2](https://www.msys2.org/)
  - Make sure you uncheck `Run MSYS2 now.`
- Launch the `MSYS2 MINGW64` shell from the start menu
  - NOTE: The `MSYS2` installer will launch the `UCRT64` shell by default if
    you didn't uncheck `Run MSYS2 now` as instructed.  That shell will not work,
    so close it if you forgot to uncheck it in the installer.
- From within the `MSYS2 MINGW64` shell enter the following commands to install
  `gcc`, `git`, `go`, `unzip`, the light OpenCL SDK, and the `gominer` source code
  - `pacman -S mingw-w64-x86_64-gcc mingw-w64-x86_64-tools mingw-w64-x86_64-go git unzip`
  - `wget https://github.com/GPUOpen-LibrariesAndSDKs/OCL-SDK/files/1406216/lightOCLSDK.zip`
  - `unzip -d /c/appsdk lightOCLSDK.zip`
  - `git clone https://github.com/decred/gominer`
- **Close the `MSYS2 MINGW64` shell and relaunch it**
  - NOTE: This is necessary to ensure all of the new environment variables are set properly
- Go to the appropriate section for either NVIDIA or AMD depending on which type of GPU you have

##### OpenCL with NVIDIA

- Build gominer
  - `cd ~/gominer`
  - `go build -tags opencl`
- Test `gominer` detects your GPU
  - `./gominer -l`

##### OpenCL with AMD

- Change to the library directory C:\appsdk\lib\x86_64
  * `cd /c/appsdk/lib/x86_64`
- Copy and prepare the ADL library for linking
  - `cp /c/Windows/SysWOW64/atiadlxx.dll .`
  - `gendef atiadlxx.dll`
  - `dlltool --output-lib libatiadlxx.a --input-def atiadlxx.def`
- Build gominer
  - `cd ~/gominer`
  - `go build -tags opencl`
- Test `gominer` detects your GPU
  - `./gominer -l`

#### CUDA Build Instructions (NVIDIA only)

**NOTE**: The CUDA version of the `gominer` is not yet compatible with
Windows.