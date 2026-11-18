package tunswap

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/tun"
)

// fakeTUN delivers packets from in, records writes, and fails both once closed.
type fakeTUN struct {
	name    string
	mtu     int
	in      chan []byte
	events  chan tun.Event
	mu      sync.Mutex
	written [][]byte
	closed  chan struct{}
	once    sync.Once
}

func newFake(name string, mtu int) *fakeTUN {
	return &fakeTUN{
		name: name, mtu: mtu,
		in: make(chan []byte, 4), events: make(chan tun.Event, 1), closed: make(chan struct{}),
	}
}

func (f *fakeTUN) File() *os.File { return nil }

func (f *fakeTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	select {
	case pkt := <-f.in:
		sizes[0] = copy(bufs[0][offset:], pkt)
		return 1, nil
	case <-f.closed:
		return 0, os.ErrClosed
	}
}

func (f *fakeTUN) Write(bufs [][]byte, offset int) (int, error) {
	select {
	case <-f.closed:
		return 0, os.ErrClosed
	default:
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range bufs {
		f.written = append(f.written, append([]byte(nil), b[offset:]...))
	}
	return len(bufs), nil
}

func (f *fakeTUN) MTU() (int, error)        { return f.mtu, nil }
func (f *fakeTUN) Name() (string, error)    { return f.name, nil }
func (f *fakeTUN) Events() <-chan tun.Event { return f.events }
func (f *fakeTUN) BatchSize() int           { return 1 }

func (f *fakeTUN) Close() error {
	f.once.Do(func() {
		close(f.closed)
		close(f.events)
	})
	return nil
}

func (f *fakeTUN) isClosed() bool {
	select {
	case <-f.closed:
		return true
	default:
		return false
	}
}

func (f *fakeTUN) writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.written)
}

func readOne(t *testing.T, d *Device) string {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		bufs := [][]byte{make([]byte, 64)}
		sizes := []int{0}
		n, err := d.Read(bufs, sizes, 0)
		if err != nil || n != 1 {
			got <- "error"
			return
		}
		got <- string(bufs[0][:sizes[0]])
	}()
	select {
	case s := <-got:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("read did not return")
		return ""
	}
}

func TestReadBlockedOnOldMovesToNew(t *testing.T) {
	oldDev, newDev := newFake("old", 1380), newFake("new", 1280)
	d := Wrap(oldDev)
	defer d.Close()

	result := make(chan string, 1)
	go func() { result <- readOne(t, d) }()
	time.Sleep(20 * time.Millisecond) // let the read block on the old TUN

	if err := d.Swap(newDev); err != nil {
		t.Fatalf("swap: %v", err)
	}
	if !oldDev.isClosed() {
		t.Fatal("old TUN still open after swap")
	}
	newDev.in <- []byte("hello")
	if got := <-result; got != "hello" {
		t.Fatalf("read got %q, want hello", got)
	}
}

func TestSwapAnnouncesMTUAndForwardsMTU(t *testing.T) {
	d := Wrap(newFake("old", 1380))
	defer d.Close()
	if err := d.Swap(newFake("new", 1280)); err != nil {
		t.Fatalf("swap: %v", err)
	}
	select {
	case ev := <-d.Events():
		if ev&tun.EventMTUUpdate == 0 {
			t.Fatalf("event %v, want an MTU update", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no MTU update after swap")
	}
	if mtu, _ := d.MTU(); mtu != 1280 {
		t.Fatalf("MTU %d, want the new TUN's 1280", mtu)
	}
	if name, _ := d.Name(); name != "new" {
		t.Fatalf("name %q, want new", name)
	}
}

func TestWriteGoesToCurrent(t *testing.T) {
	oldDev, newDev := newFake("old", 1380), newFake("new", 1380)
	d := Wrap(oldDev)
	defer d.Close()
	if err := d.Swap(newDev); err != nil {
		t.Fatalf("swap: %v", err)
	}
	if _, err := d.Write([][]byte{[]byte("pkt")}, 0); err != nil {
		t.Fatalf("write: %v", err)
	}
	if oldDev.writes() != 0 || newDev.writes() != 1 {
		t.Fatalf("writes old=%d new=%d, want 0 and 1", oldDev.writes(), newDev.writes())
	}
}

func TestCloseEndsReadAndRefusesSwap(t *testing.T) {
	cur := newFake("cur", 1380)
	d := Wrap(cur)

	errc := make(chan error, 1)
	go func() {
		_, err := d.Read([][]byte{make([]byte, 64)}, []int{0}, 0)
		errc <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("read after close returned no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read kept blocking after close")
	}
	if _, open := <-d.Events(); open {
		t.Fatal("events channel still open after close")
	}

	late := newFake("late", 1380)
	if err := d.Swap(late); !errors.Is(err, ErrClosed) {
		t.Fatalf("swap after close: %v, want ErrClosed", err)
	}
	if !late.isClosed() {
		t.Fatal("swap after close left the new TUN open")
	}
}
