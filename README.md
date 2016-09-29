# gominer

gominer is an application for performing Proof-of-Work (PoW) mining on the
Decred network.  It supports solo and stratum/pool mining using CUDA and
OpenCL devices.

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

## Linux Build Pre-Requisites

You will either need to install CUDA for NVIDIA graphics cards or OpenCL
library/headers that support your device such as: AMDGPU-PRO (for newer AMD
cards), Beignet (for Intel Graphics), or Catalyst (for older AMD cards).

For example, on Ubuntu 16.04 you can install the necessary OpenCL packages (for
Intel Graphics) and CUDA libraries with:

```
sudo apt-get install beignet-dev nvidia-cuda-dev nvidia-cuda-toolkit
```

gominer has been built successfully on Ubuntu 16.04 with go1.6.2, go1.7.1,
g++ 5.4.0, and beignet-dev 1.1.1-2 although other combinations should work as
well.

## Linux Build Instructions

To download and build gominer, run:

```
go get -u github.com/Masterminds/glide
mkdir -p $GOPATH/src/github.com/decred
cd $GOPATH/src/github.com/decred
git clone  https://github.com/decred/gominer.git
cd gominer
glide install
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

## Windows Build Pre-Requisites

- Download and install the official Go Windows binaries from [https://golang.dl/](https://golang.org/dl/)
- Download and install Git for Windows from [https://git-for-windows.github.io/](https://git-for-windows.github.io/)
  * Make sure to select the Git-Bash option when prompted
- Download the MinGW-w64 installer from [https://sourceforge.net/projects/mingw-w64/files/Toolchains targetting Win32/Personal Builds/mingw-builds/installer/](https://sourceforge.net/projects/mingw-w64/files/Toolchains%20targetting%20Win32/Personal%20Builds/mingw-builds/installer/)
  * Select the x64 toolchain and use defaults for the other questions
- Set the environment variable GOPATH to C:\Users\username\go
- Check that the GOROOT environment variable is set to C:\Go
  * This should have been done by the Go installer
- Add the following locations to your PATH C:\Users\username\go\bin;C:\Go\bin
- Add C:\Program Files\mingw-w64\x84_64-6.2.0-posix-seh-rt_v5-rev1\mingw64\bin to your PATH (This is the latest release as of 2016-09-29)
- go get github.com/Masterminds/glide
  * You should be able to type ```glide``` and get glide's usage display.  If not, double check the steps above
- go get github.com/decred/gominer
  * Compilation will most likely fail which can be safely ignored for now.
- Change to the gominer directory
  * ```cd $GOPATH/src/github.com/decred/gominer```
- Install dependencies via glide
  * ```glide install```

### CUDA Specific Steps

- Download Microsoft Visual Studio 2013 from [https://www.microsoft.com/en-us/download/details.aspx?id=44914](https://www.microsoft.com/en-us/download/details.aspx?id=44914)
- Add C:\Program Files (x86)\Microsoft Visual Studio 12.0\VC\bin to your PATH
- Install CUDA 7.0 from [https://developer.nvidia.com/cuda-toolkit-70](https://developer.nvidia.com/cuda-toolkit-70)
- Add C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v7.0\bin to your PATH

### OpenCL/ADL Specific Steps

- Download AMD APP SDK v3.0 from [http://developer.amd.com/tools-and-sdks/opencl-zone/amd-accelerated-parallel-processing-app-sdk/](http://developer.amd.com/tools-and-sdks/opencl-zone/amd-accelerated-parallel-processing-app-sdk/)
  * Samples may be unselected from the install to save space as only the libraries and headers are needed
- Copy or Move C:\Program Files (x86)\AMD APP SDK\3.0 to C:\appsdk
  * Ensure the folders C:\appsdk\include and C:\appsdk\lib are populated
- Change to the library directory C:\appsdk\lib\x86_64
  * ```cd C:\appsdk\lib\x86_64```
- Copy and prepare the ADL library for linking
  * ```copy c:\Windows\SysWOW64\atiadlxx.dll .```
  * ```gendef atiadlxx.dll```
  * ```dlltool --output-lib libatiadlxx.a --input-def atiadlxx.def```

## Windows Build Instructions

### CUDA

- Using git-bash:
  * ```cd $GOPATH/src/github.com/decred/gominer```
  * ```mingw32-make.exe```
- Copy dependencies:
  * ```copy obj/decred.dll .```
  * ```copy nvidia/NVSMI/nvml.dll .```

### OpenCL / OpenCL w/ADL support

- For OpenCL:
  * ```go build -tags opencl```

- For OpenCL with AMD Device Library (ADL) support:
  * ```go build -tags opencladl```
