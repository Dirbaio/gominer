# gominer

## Installation

You need to have OpenCL installed. To download and build gominer, run:

    go get github.com/decred/gominer

On Ubuntu 16.04 you can install the necessary OpenCL packages (for
Intel Graphics cards) with

    sudo apt-get install beignet-dev

Other graphics cards will need different libraries.  We have built
successfully on Ubuntu 16.04 with go1.6.2, g++ 5.4.0 and
beignet-dev 1.1.1-2 although other combinations should work as well.

## Running

Run for benchmark:

    gominer -B

Run for real mining:

    gominer -u myusername -P hunter2

To mine on a pool:

    gominer -o stratum+tcp://pool:port -m username -n password

