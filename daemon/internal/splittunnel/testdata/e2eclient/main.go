//go:build linux

// Command e2eclient is the app the split-tunnel e2e test excludes (or not): one static
// binary copied to several paths, with a launcher mode that runs the copies outside the test's tree.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var server = [4]byte{10, 99, 0, 1}

const echoPort = 8080

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: app oneshot|dns|bigudp|spawn|hold|launcher ...")
		os.Exit(2)
	}
	fmt.Printf("PID %d PPID %d MODE %s\n", os.Getpid(), os.Getppid(), os.Args[1])
	var err error
	switch os.Args[1] {
	case "oneshot":
		oneshot(os.Args[2])
	case "dns":
		dns(os.Args[2])
	case "bigudp":
		size, _ := strconv.Atoi(os.Args[3])
		bigUDP(os.Args[2], size)
	case "spawn":
		err = spawn(os.Args[2], os.Args[3:])
	case "hold":
		secs, _ := strconv.Atoi(os.Args[3])
		hold(os.Args[2], time.Duration(secs)*time.Second)
	case "launcher":
		err = launcher(os.Args[2])
	default:
		err = fmt.Errorf("unknown mode %q", os.Args[1])
	}
	if err != nil {
		fmt.Println("FATAL", err)
		os.Exit(1)
	}
}

// dialTCP binds first so the local port is known even when the connect times out.
func dialTCP(timeout time.Duration) (net.Conn, int, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, 0, err
	}
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_SNDTIMEO, &tv); err != nil {
		syscall.Close(fd)
		return nil, 0, err
	}
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{}); err != nil {
		syscall.Close(fd)
		return nil, 0, err
	}
	lport := 0
	if sa, err := syscall.Getsockname(fd); err == nil {
		lport = sa.(*syscall.SockaddrInet4).Port
	}
	fmt.Printf("TCP LPORT %d\n", lport)
	if err := syscall.Connect(fd, &syscall.SockaddrInet4{Port: echoPort, Addr: server}); err != nil {
		syscall.Close(fd)
		if errors.Is(err, syscall.EINPROGRESS) || errors.Is(err, syscall.EAGAIN) {
			err = fmt.Errorf("connect timeout after %v", timeout)
		}
		return nil, lport, err
	}
	f := os.NewFile(uintptr(fd), "tcp")
	c, err := net.FileConn(f)
	f.Close()
	return c, lport, err
}

func tcpHello(tag string) (net.Conn, bool) {
	c, _, err := dialTCP(3 * time.Second)
	if err != nil {
		fmt.Printf("TCP ERR %v\n", err)
		return nil, false
	}
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprintf(c, "HELLO %s\n", tag); err != nil {
		fmt.Printf("TCP ERR write: %v\n", err)
		c.Close()
		return nil, false
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		fmt.Printf("TCP ERR read: %v\n", err)
		c.Close()
		return nil, false
	}
	fmt.Printf("TCP OK %s\n", strings.TrimSpace(line))
	c.SetDeadline(time.Time{})
	return c, true
}

func udpExchange(label string, port int, payload []byte, wait time.Duration) ([]byte, error) {
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IP(server[:]), Port: port})
	if err != nil {
		return nil, err
	}
	defer c.Close()
	fmt.Printf("%s LPORT %d\n", label, c.LocalAddr().(*net.UDPAddr).Port)
	if _, err := c.Write(payload); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	c.SetReadDeadline(time.Now().Add(wait))
	buf := make([]byte, 65535)
	n, err := c.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func oneshot(tag string) {
	if c, ok := tcpHello(tag + "-tcp"); ok {
		c.Close()
	}
	reply, err := udpExchange("UDP", echoPort, []byte("HELLO "+tag+"-udp"), 2*time.Second)
	if err != nil {
		fmt.Printf("UDP ERR %v\n", err)
		return
	}
	fmt.Printf("UDP OK %s\n", strings.TrimSpace(string(reply)))
}

func dns(tag string) {
	reply, err := udpExchange("DNS", 53, []byte("DNS "+tag), 1500*time.Millisecond)
	if err != nil {
		fmt.Printf("DNS ERR %v\n", err)
		return
	}
	fmt.Printf("DNS OK %q\n", reply)
}

func bigUDP(tag string, size int) {
	head := fmt.Sprintf("BIG %s PAD\n", tag)
	payload := make([]byte, size)
	copy(payload, head)
	for i := len(head); i < size; i++ {
		payload[i] = byte(i % 251)
	}
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:8])
	fmt.Printf("BIG SENT %d SHA %s\n", size, want)
	reply, err := udpExchange("BIG", echoPort, payload, 3*time.Second)
	if err != nil {
		fmt.Printf("BIG ERR %v\n", err)
		return
	}
	line, _, _ := bytes.Cut(reply, []byte("\n"))
	fmt.Printf("BIG OK replyLen=%d shaMatch=%v line=%s\n", len(reply), strings.Contains(string(line), "SHA "+want), line)
}

func spawn(path string, args []string) error {
	var out bytes.Buffer
	cmd := exec.Command(path, args...)
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Printf("SPAWN child=%d path=%s\n", cmd.Process.Pid, path)
	err := cmd.Wait()
	for _, l := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		fmt.Printf("CHILD| %s\n", l)
	}
	fmt.Printf("CHILD EXIT %v\n", err)
	return nil
}

func hold(tag string, d time.Duration) {
	c, ok := tcpHello(tag)
	if !ok {
		return
	}
	defer c.Close()
	fmt.Println("HOLDING")
	c.SetReadDeadline(time.Now().Add(d))
	n, err := c.Read(make([]byte, 64))
	switch {
	case n > 0:
		fmt.Printf("HOLD DATA %d\n", n)
	case errors.Is(err, syscall.ECONNRESET):
		fmt.Printf("HOLD RESET %v\n", err)
	case errors.Is(err, io.EOF):
		fmt.Println("HOLD EOF")
	case errors.Is(err, os.ErrDeadlineExceeded):
		fmt.Println("HOLD TIMEOUT")
	default:
		fmt.Printf("HOLD ERR %v\n", err)
	}
}

type launchReq struct {
	Argv      []string `json:"argv"`
	TimeoutMs int      `json:"timeoutMs"`
}

type launchResp struct {
	Out  string `json:"out"`
	Err  string `json:"err"`
	PID  int    `json:"pid"`
	Took int64  `json:"tookMs"`
}

func launcher(sock string) error {
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	fmt.Println("LAUNCHER READY", sock)
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go serveLaunch(c)
	}
}

func serveLaunch(c net.Conn) {
	defer c.Close()
	var req launchReq
	if err := json.NewDecoder(c).Decode(&req); err != nil || len(req.Argv) == 0 {
		return
	}
	if req.TimeoutMs <= 0 {
		req.TimeoutMs = 30000
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.TimeoutMs)*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...)
	cmd.Stdout, cmd.Stderr = &out, &out
	start := time.Now()
	var resp launchResp
	if err := cmd.Start(); err != nil {
		resp.Err = err.Error()
	} else {
		resp.PID = cmd.Process.Pid
		if err := cmd.Wait(); err != nil {
			resp.Err = err.Error()
		}
	}
	resp.Out = out.String()
	resp.Took = time.Since(start).Milliseconds()
	_ = json.NewEncoder(c).Encode(resp)
}
