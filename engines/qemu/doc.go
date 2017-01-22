// Package qemuengine implements a QEMU based engine for taskcluster-worker.
//
// This package requires following debian packages:
//  - qemu
//  - iproute2
//  - dnsmasq-base
// This is tested against Debian Jessie 64bit, should probably work with most
// other systems.
package qemuengine

import "github.com/walac/taskcluster-worker/runtime/util"

var debug = util.Debug("qemu")
