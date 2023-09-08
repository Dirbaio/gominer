# Manual CUDA Build on Windows

Building the CUDA-enabled `gominer` on a Windows platform is tricky, requires
several GB worth of downloads and the [automatic builder
script](/cuda_builder.go) that is called when running `go generate -tags cuda`
is not guaranteed to work on every combination of setups.

The main difficulties when building the CUDA version on windows are:

- The installers for CUDA Toolkit and MSVC do not put their respective binaries
  in the user's `$PATH` environment variable.
- A default installation of MSYS2 ignores the global and user's `$PATH` anyway.
- Having files in `c:\Program Files\` (path with a space) causes all sorts of
  issues in all layers of the compilation stack.  In particular, [cgo directives
  do not work](https://github.com/golang/go/issues/45637) with such paths.
- An [outdated CUDA package](https://github.com/barnex/cuda5) currently used by
  `gominer` has wrong cgo include paths in any case.
- CUDA requires `cl.exe` (MSVC compiler) to build its binary kernels, but Go
  does not accept it as a possible `cgo` compiler.


All of these must be resolved in other to correctly build the CUDA version of
`gominer` on Windows.  The automatic builder script uses a few tricks and some
reasonable assumptions to attempt to solve building for the most common install
scenarios.  But for the cases where that is not sufficient, the following
document lists a general procedure for setting the required Windows environment.

## Install the Required Tools

- Download and install the appropriate NVIDIA driver
  - https://www.nvidia.com/download/index.aspx
- Download and install the NVIDIA CUDA Toolkit:
  - https://developer.nvidia.com/cuda-toolkit
- Download and install Microsoft Visual Studio:
  - https://visualstudio.microsoft.com/vs/community/
  - Ensure the "Desktop Development with C++" component will be installed

Restart the computer and ensure Windows can recognize the NVIDIA GPU as such.

## Setup MSYS2

While it _may_ be possible to build on PowerShell or even `cmd.exe`, going
through MSYS2 means the changes are more explicit and generally don't require
clicking through GUIs to find settings.

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


## Create junction to NVIDIA Toolkit

Now comes the tricky bit. `cgo` flags don't currently support paths with spaces
in them. Therefore, we'll create a junction to the NVIDIA Toolkit install path
in order to be able to specify it.

- Determine the NVIDIA Toolkit install path.
  - This is generally `c:\Program Files\NVIDIA GPU Toolkit\CUDA\vXX.Y`
- Create a junction for the target directory from within MSYS2:
  - `cd gominer`
  - `cmd.exe //c mklink //J nvidia "c:\Program Files\NVIDIA GPU Toolkit\CUDA\vXX.Y"`
  - Replace `vXX.Y` with the version of the toolkit actually installed
- Copy the file `bin/cudart64_xx.dll` to `gominer`'s dir
  - This file will be needed by `gominer` once built
  - Replace XX with the major CUDA toolkit version
  - `cp nvidia/bin/cudart64_*.dll .`

If the toolkit is ever updated to a newer version, repeat this process. To
remove the junction after it is no longer needed, execute `rm nvidia`


## Determine the MSVC `cl.exe` location

Visual Studio versions after 2017 support multiple installs on the same machine,
each with different sets of components and features installed.  Determine which
version contains the "Desktop Development with C++" component you wish to use
and locate the path to its `cl.exe` compiler.

For example, for Visual Studio 2022 Community edition, building x64 targets
in x64 host computers (the most common case), the path to `cl.exe` is
`C:\Program Files\Microsoft Visual Studio\2022\Community\VC\Tools\MSVC\14.37.32822\bin\Hostx64\x64`.

The important bit is that this dir *MUST* contain the `cl.exe` compiler. Modify
the various versions as applicable to your specific computer.  Note down this
dir, as it will be used in the next step.

## Setup the Environment Variables

Edit MSYS2's `.bash_profile` file to add the required environment variables.
Any text editor may be used, as long as it supports Unix style line endings:

- Edit `~/.bash_profile`
  - If using a native Windows editor, the file is generally located at
    `C:\msys64\home\[username]`
- Add the following lines at the end of the file:

```
export CL_PATH="<path-to-cl.exe-found-in-previous-step>"
export CGO_CFLAGS="$CGO_CFLAGS -Invidia/include"
export CGO_LDFLAGS="$CGO_CFLAGS -Lnvidia/lib"
export PATH="$CL_PATH:$PATH"
```

Restart the MSYS2 shell after editing the file. To know whether the values are
being used, issue `echo` commands (for example, `echo $CGO_CFLAGS`).

## Build `blake3.dll` using `nvcc`

Now with the environment fully setup, the Blake3 CUDA kernel may be built:

```
$ nvcc --shared --optimize=3 --compiler-options=-GS-,-MD -I. blake3.cu -o blake3.dll
```

The end result is that a `blake3.dll` shared library should be created in
`gominer`'s root dir.

Several warnings may be displayed, (specially regarding use of deprecated calls)
but those may be safely ignored.

## Build `gominer.exe`

Finally, with `blake3.dll` created, `gominer` may be buit in the standard way:

```
$ go build -tags cuda .
$ ./gominer -l
```

Continue with the standard [configure and run](../#configuring-gominer) procedure.

### Troubleshooting `gominer.exe` does not run

If after building `gominer.exe`, it exists in the dir but executing (with `-l`
or `-h`) does not display anything in the terminal, this is usually a sign of
missing dlls.

Run the following command:

```
$ ldd gominer.exe
```

And search the output for any DLLs listed as "not found". Install any missing
dependency or copy the DLL directly to `gominer`s dir.

