# gominer

## Installation

You need to have the OpenCL or CUDA development libraries
installed (depending on which version of gominer you would like to
build) . You also need the runtime and drivers for the one you plan
on running (CUDA for nvidia, OpenCL for anything) To download and
build gominer, run:

```
go get -u github.com/Masterminds/glide
mkdir -p $GOPATH/src/github.com/decred
cd $GOPATH/src/github.com/decred
git clone  https://github.com/decred/gominer.git
cd gominer
glide i
```

For OpenCL:
```
go install -tags 'opencl'
```

For OpenCL with AMD Device Library (ADL) support:
```
go install -tags 'opencladl'
```

For CUDA with NVIDIA Management Library (NVML) support:
```
make
go install -tags 'cuda'
```

On Ubuntu 16.04 you can install the necessary OpenCL packages (for
Intel Graphics cards) and CUDA libraries with:

```
sudo apt-get install beignet-dev nvidia-cuda-dev nvidia-cuda-toolkit
```

Other graphics cards will need different libraries.  We have built
successfully on Ubuntu 16.04 with go1.6.2, go1.7.1, g++ 5.4.0 and
beignet-dev 1.1.1-2 although other combinations should work as well.

## Running

Run for benchmark:

```
gominer -B
```

Run for real mining:

```
gominer -u myusername -P hunter2
```

To mine on a pool:

```
gominer -o stratum+tcp://pool:port -m username -n password
```
