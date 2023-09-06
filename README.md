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

#### Pre-Requisites

You will either need to install CUDA for NVIDIA graphics cards or OpenCL
library/headers that support your device such as: AMDGPU-PRO (for newer AMD
cards), Beignet (for Intel Graphics), or Catalyst (for older AMD cards).

For example, on Ubuntu 23.04 you can install the necessary OpenCL packages (for
Intel Graphics) and CUDA libraries with:

```
sudo apt-get install nvidia-cuda-dev nvidia-cuda-toolkit
```

gominer has been built successfully on Ubuntu 23.04 with go1.21.0,
g++ 5.4.0 although other combinations should work as well.

#### Instructions

To download and build gominer, run:

```
git clone https://github.com/decred/gominer
cd gominer
```

For CUDA with NVIDIA Management Library (NVML) support:
```
make
```

For OpenCL (autodetects AMDGPU support):
```
go build -tags opencl
```

For OpenCL with AMD Device Library (ADL) support:
```
go build -tags opencladl
```

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
  `gcc`, `git`, `go`, `unzip`, and the `gominer` source code
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

**NOTE**: The CUDA version of the Blake3 gominer is not yet compatible with
Windows.

##### Pre-Requisites

- Download Microsoft Visual Studio 2013 from [https://www.microsoft.com/en-us/download/details.aspx?id=44914](https://www.microsoft.com/en-us/download/details.aspx?id=44914)
- Add `C:\Program Files (x86)\Microsoft Visual Studio 12.0\VC\bin` to your PATH
- Install CUDA 7.0 from [https://developer.nvidia.com/cuda-toolkit-70](https://developer.nvidia.com/cuda-toolkit-70)
- Add `C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v7.0\bin` to your PATH

###### Steps
- Using git-bash:
  * ```cd $GOPATH/src/github.com/decred/gominer```
  * ```mingw32-make.exe```
- Copy dependencies:
  * ```copy obj/decred.dll .```
  * ```copy nvidia/NVSMI/nvml.dll .```
