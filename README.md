# csi-driver-synced-hostpath

A Kubernetes CSI driver, similar to the [csi-driver-hostpath](https://github.com/kubernetes-csi/csi-driver-host-path), that allows to mount volumes on more than one `Node`.

Disclaimer: the driver is not production-ready. However it should be safe enough to use in a "home-lab" on a Kubernetes cluster running with multiple nodes.

## Building

* The `make` command (e.g. [GNU make](https://www.gnu.org/software/make/manual/make.html)).
* The [Golang toolchain](https://golang.org/doc/install) (version 1.25 or later).

In a shell, execute: `make` (or `make build`).

The build artifacts can be cleaned by using: `make clean`.

## TODOs

* File client/server authentication
* File upload "policies": archive more than 1 version.
* File download/upload compression?

