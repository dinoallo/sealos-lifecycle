# Environment settings

sealos only support Linux now, you need a Linux server to test it.

Some tools can be very handy to help you start a virtual machine such as [multipass](https://multipass.run/)

## Install golang

```shell
wget https://go.dev/dl/go1.20.linux-amd64.tar.gz
tar -C /usr/local -zxvf go1.20.linux-amd64.tar.gz
cat >> /etc/profile <<EOF
# set go path
export PATH=\$PATH:/usr/local/go/bin
EOF
source /etc/profile  && go version
```

## Build the project

```shell script
git clone https://github.com/labring/sealos.git
cd sealos
make build BINS=sealos
```

You can scp the bin file to your Linux host.

If you use multipass, you can mount the bin dir to the vm:

```shell script
multipass mount /your-bin-dir <name>[:<path>]
```

Then test it locally.

## Notes about cross-platform building

All the binaries except `sealos` can be built anywhere since they have `CGO_ENABLED=0`. However, `sealos` needs to support overlay driver when running some subcommands like `images`, which relies on CGO. Therefore, CGO is switched on when building `sealos`, making it impossible to build `sealos` binaries on platforms other than Linux.

> Both Makefile and GoReleaser in this project have this setting.

## Notes about optional btrfs support

The default Makefile build tags exclude the btrfs graphdriver so local builds and coverage runs do not require
`btrfs` development headers.

If you need btrfs support in a build, opt in explicitly:

```shell
make build ENABLE_BTRFS=1
```

## Notes about go workspace

As sealos is using go1.18's [workspace feature](https://go.dev/doc/tutorial/workspaces), once you add a new module, you need to run `go work use -r .` at root directory to update the workspace synced.

## Example: how to build sealos on macOS(ARM64) using multipass

1. launch vm and mount sealos source code:
```shell
# edit the SEALOS_CODE_DIR to your own
export SEALOS_CODE_DIR=/Users/fanux/work/src/github.com/labring/sealos
# copy, paste and run to launch vm
multipass launch \
   --mount ${SEALOS_CODE_DIR}:/go/src/github.com/labring/sealos \
   --name sealos-dev --cpus 2 --mem 4G --disk 40G
```

2. exec into the vm
```shell
multipass exec sealos-dev bash
sudo su
```
3. install golang
```shell
apt-get install build-essential
apt install make
wget https://go.dev/dl/go1.20.linux-arm64.tar.gz
tar -C /usr/local -zxvf go1.20.linux-arm64.tar.gz
cat >> /etc/profile <<EOF
# set go path
export PATH=\$PATH:/usr/local/go/bin
EOF
source /etc/profile  && go version
```
4. Build the source code
```shell
go env -w GOPROXY=https://goproxy.cn,direct # optional
make build
```

## FAQ

1. clone code slow, your can use ghproxy: `git clone https://ghproxy.com/https://github.com/labring/sealos`
2. build download package slow, you can use goproxy: `go env -w GOPROXY=https://goproxy.cn,direct && make build`
3. If CGO builds cannot find a compiler, install a C toolchain such as `gcc` / `build-essential`, or override the compiler explicitly, for example: `make build BINS=sealos CC=gcc` or `make build.multiarch BINS=sealos CC_arm64=aarch64-linux-gnu-gcc`
