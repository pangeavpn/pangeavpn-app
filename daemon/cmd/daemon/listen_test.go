package main

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestDescribeListenError_NamesTheDaemonAlreadyOnThePort(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer held.Close()

	addr := held.Addr().String()
	_, listenErr := net.Listen("tcp", addr)
	if listenErr == nil {
		t.Fatal("expected the second listen on the same address to fail")
	}

	msg := describeListenError(addr, listenErr)
	if !strings.Contains(msg, "another daemon") {
		t.Fatalf("port conflict must be named as such, got: %s", msg)
	}
	if !strings.Contains(msg, addr) {
		t.Fatalf("message must name the address, got: %s", msg)
	}
}

func TestDescribeListenError_PassesOtherFailuresThrough(t *testing.T) {
	msg := describeListenError("127.0.0.1:8787", errors.New("permission denied"))
	if strings.Contains(msg, "another daemon") {
		t.Fatalf("unrelated failure misreported as a port conflict: %s", msg)
	}
	if !strings.Contains(msg, "permission denied") {
		t.Fatalf("underlying error lost: %s", msg)
	}
}
