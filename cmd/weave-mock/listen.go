package main

import "net"

// listen opens a TCP listener on a free port. Used by main_test.go to
// pick an ephemeral port without baking a hard-coded port into tests.
func listen() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}
