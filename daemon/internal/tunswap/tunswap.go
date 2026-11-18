// Package tunswap moves a running WireGuard device to a new TUN without a restart,
// so Android can re-establish its interface while the peer and transport stay up.
package tunswap

import (
	"errors"
	"os"
	"sync"

	"golang.zx2c4.com/wireguard/tun"
)

// ErrClosed is returned by Swap once the device has been closed.
var ErrClosed = errors.New("tunswap: device closed")

// Device forwards to the current inner TUN. A swap closes the old one at once, since
// draining it would stall the single reader on a file Android no longer feeds.
type Device struct {
	mu      sync.RWMutex
	cur     tun.Device
	closed  bool
	batch   int
	events  chan tun.Event
	done    chan struct{}
	forward sync.WaitGroup
	once    sync.Once
}

// Wrap takes ownership of dev; Close closes whichever TUN is current.
func Wrap(dev tun.Device) *Device {
	d := &Device{
		cur:    dev,
		batch:  dev.BatchSize(),
		events: make(chan tun.Event, 8),
		done:   make(chan struct{}),
	}
	d.follow(dev)
	return d
}

// follow relays one inner device's events until it closes its channel.
func (d *Device) follow(dev tun.Device) {
	d.forward.Add(1)
	go func() {
		defer d.forward.Done()
		for ev := range dev.Events() {
			select {
			case d.events <- ev:
			case <-d.done:
				return
			}
		}
	}()
}

// Swap makes next current, closes the old TUN and announces an MTU update. After
// Close it closes next and returns ErrClosed.
func (d *Device) Swap(next tun.Device) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		_ = next.Close()
		return ErrClosed
	}
	old := d.cur
	d.cur = next
	d.follow(next)
	d.mu.Unlock()

	_ = old.Close()
	select {
	case d.events <- tun.EventMTUUpdate:
	default:
		// A full queue already holds an update the device has yet to read.
	}
	return nil
}

func (d *Device) current() (tun.Device, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cur, d.closed
}

// retired reports whether dev was swapped out, so its error belongs to the swap.
func (d *Device) retired(dev tun.Device) bool {
	cur, closed := d.current()
	return !closed && cur != dev
}

// Read retries on the new TUN when a swap closed the one it was blocked on.
func (d *Device) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	for {
		dev, _ := d.current()
		n, err := dev.Read(bufs, sizes, offset)
		if err == nil || !d.retired(dev) {
			return n, err
		}
		if n > 0 {
			return n, nil
		}
	}
}

// Write tries the new TUN once when a swap closed the old one mid-write.
func (d *Device) Write(bufs [][]byte, offset int) (int, error) {
	dev, _ := d.current()
	n, err := dev.Write(bufs, offset)
	if err != nil && n == 0 && d.retired(dev) {
		dev, _ = d.current()
		return dev.Write(bufs, offset)
	}
	return n, err
}

func (d *Device) File() *os.File {
	dev, _ := d.current()
	return dev.File()
}

func (d *Device) MTU() (int, error) {
	dev, _ := d.current()
	return dev.MTU()
}

func (d *Device) Name() (string, error) {
	dev, _ := d.current()
	return dev.Name()
}

func (d *Device) Events() <-chan tun.Event { return d.events }

// BatchSize is fixed at Wrap: wireguard-go sizes its buffers once.
func (d *Device) BatchSize() int { return d.batch }

func (d *Device) Close() error {
	var err error
	d.once.Do(func() {
		d.mu.Lock()
		d.closed = true
		cur := d.cur
		d.mu.Unlock()
		close(d.done)
		err = cur.Close()
		d.forward.Wait()
		close(d.events)
	})
	return err
}
