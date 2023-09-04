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

#### Pre-Requisites

- Download and install the official Go Windows binaries from [https://golang.dl/](https://golang.org/dl/)
- Download and install Git for Windows from [https://git-for-windows.github.io/](https://git-for-windows.github.io/)
  * Make sure to select the Git-Bash option when prompted
- Download the MinGW-w64 installer from [https://sourceforge.net/projects/mingw-w64/files/Toolchains targetting Win32/Personal Builds/mingw-builds/installer/](https://sourceforge.net/projects/mingw-w64/files/Toolchains%20targetting%20Win32/Personal%20Builds/mingw-builds/installer/)
  * Select the x64 toolchain and use defaults for the other questions
- `git clone https://github.com/decred/gominer`

#### Build Instructions

##### CUDA

**NOTE**: The CUDA version of the Blake3 gominer is not yet compatible to
windows.

###### Pre-Requisites

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

##### OpenCL/ADL

###### Pre-Requisites

- Download OpenCL SDK from [https://github.com/GPUOpen-LibrariesAndSDKs/OCL-SDK/releases/tag/1.0](https://github.com/GPUOpen-LibrariesAndSDKs/OCL-SDK/releases/tag/1.0)
- Unzip or untar the downloaded `lightOCLSDK` archive to `C:\appsdk`
  * Ensure the folders `C:\appsdk\include` and `C:\appsdk\lib` are populated
- Change to the library directory C:\appsdk\lib\x86_64
  * `cd /D C:\appsdk\lib\x86_64`
- Copy and prepare the ADL library for linking
  * `copy c:\Windows\SysWOW64\atiadlxx.dll .`
  * `gendef atiadlxx.dll`
  * `dlltool --output-lib libatiadlxx.a --input-def atiadlxx.def`

###### Steps

- For OpenCL:
  * `cd gominer`
  * `go build -tags opencl`

- For OpenCL with AMD Device Library (ADL) support:
  * `cd gominer`
  * `go build -tags opencladl`
