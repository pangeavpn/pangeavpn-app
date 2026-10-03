package splittunnel

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/sagernet/gvisor/pkg/buffer"
	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/header"
	"github.com/sagernet/gvisor/pkg/tcpip/network/ipv4"
	"github.com/sagernet/gvisor/pkg/tcpip/stack"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/tcp"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/udp"
)

const (
	engineNIC     tcpip.NICID = 1
	forwarderWnd              = 64 << 10
	forwarderMax              = 1024
	tcpBufDefault             = 32 << 10
	tcpBufMax                 = 256 << 10
)

// linkEndpoint has no queue: inbound packets are delivered on the caller's goroutine and
// outbound batches go straight to the inner TUN under the device write mutex.
type linkEndpoint struct {
	d   *Device
	mtu atomic.Uint32

	mu         sync.RWMutex
	dispatcher stack.NetworkDispatcher
}

var _ stack.LinkEndpoint = (*linkEndpoint)(nil)

func newLinkEndpoint(d *Device, mtu uint32) *linkEndpoint {
	l := &linkEndpoint{d: d}
	l.mtu.Store(mtu)
	return l
}

func (l *linkEndpoint) MTU() uint32                             { return l.mtu.Load() }
func (l *linkEndpoint) SetMTU(mtu uint32)                       { l.mtu.Store(mtu) }
func (l *linkEndpoint) MaxHeaderLength() uint16                 { return 0 }
func (l *linkEndpoint) LinkAddress() tcpip.LinkAddress          { return "" }
func (l *linkEndpoint) SetLinkAddress(tcpip.LinkAddress)        {}
func (l *linkEndpoint) Wait()                                   {}
func (l *linkEndpoint) ARPHardwareType() header.ARPHardwareType { return header.ARPHardwareNone }
func (l *linkEndpoint) AddHeader(*stack.PacketBuffer)           {}
func (l *linkEndpoint) ParseHeader(*stack.PacketBuffer) bool    { return true }
func (l *linkEndpoint) Close()                                  {}
func (l *linkEndpoint) SetOnCloseAction(func())                 {}

// Capabilities never claims TX offload, so gVisor computes every checksum it emits.
func (l *linkEndpoint) Capabilities() stack.LinkEndpointCapabilities {
	return stack.CapabilityRXChecksumOffload
}

func (l *linkEndpoint) Attach(dispatcher stack.NetworkDispatcher) {
	l.mu.Lock()
	l.dispatcher = dispatcher
	l.mu.Unlock()
}

func (l *linkEndpoint) IsAttached() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.dispatcher != nil
}

// deliver hands one IPv4 packet to the stack; the payload is copied, so pkt may be reused.
func (l *linkEndpoint) deliver(pkt []byte) {
	l.mu.RLock()
	disp := l.dispatcher
	l.mu.RUnlock()
	if disp == nil {
		return
	}
	pb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(pkt)})
	disp.DeliverNetworkPacket(ipv4.ProtocolNumber, pb)
	pb.DecRef()
}

type writeBatch struct {
	bufs []*packetBuf
	raw  [][]byte
}

var writeBatchPool = sync.Pool{New: func() any { return new(writeBatch) }}

func (l *linkEndpoint) WritePackets(list stack.PacketBufferList) (int, tcpip.Error) {
	pkts := list.AsSlice()
	if len(pkts) == 0 {
		return 0, nil
	}
	wb := writeBatchPool.Get().(*writeBatch)
	for _, pkt := range pkts {
		p := newPacketBuf(writeHeadroom + pkt.Size())
		off := writeHeadroom
		for _, s := range pkt.AsSlices() {
			off += copy(p.data[off:], s)
		}
		wb.bufs = append(wb.bufs, p)
		wb.raw = append(wb.raw, p.data)
	}
	err := l.d.writeInjected(wb.raw)
	for _, p := range wb.bufs {
		p.release()
	}
	clear(wb.bufs)
	clear(wb.raw)
	wb.bufs, wb.raw = wb.bufs[:0], wb.raw[:0]
	writeBatchPool.Put(wb)
	if err != nil {
		return 0, &tcpip.ErrClosedForSend{}
	}
	return len(pkts), nil
}

// newEngineStack is a variable so tests can make stack creation fail.
var newEngineStack = newStack

func newStack(link *linkEndpoint, lim limits, handler func(*tcp.ForwarderRequest)) (*stack.Stack, error) {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	fail := func(what string, err tcpip.Error) (*stack.Stack, error) {
		s.Close()
		return nil, errors.New(what + ": " + err.String())
	}
	if err := s.CreateNICWithOptions(engineNIC, link, stack.NICOptions{}); err != nil {
		return fail("create nic", err)
	}
	s.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: engineNIC}})
	if err := s.SetSpoofing(engineNIC, true); err != nil {
		return fail("spoofing", err)
	}
	if err := s.SetPromiscuousMode(engineNIC, true); err != nil {
		return fail("promiscuous", err)
	}
	sack := tcpip.TCPSACKEnabled(true)
	moderate := tcpip.TCPModerateReceiveBufferOption(true)
	rcv := tcpip.TCPReceiveBufferSizeRangeOption{Min: tcp.MinBufferSize, Default: tcpBufDefault, Max: tcpBufMax}
	snd := tcpip.TCPSendBufferSizeRangeOption{Min: tcp.MinBufferSize, Default: tcpBufDefault, Max: tcpBufMax}
	timeWait := tcpip.TCPTimeWaitTimeoutOption(lim.timeWait)
	finLinger := tcpip.TCPLingerTimeoutOption(lim.finLinger)
	opts := []tcpip.SettableTransportProtocolOption{&sack, &moderate, &rcv, &snd, &timeWait, &finLinger}
	if runtime.GOOS == "windows" {
		// RACK on the gVisor sender caused ~87% spurious retransmits against wintun.
		recovery := tcpip.TCPRecovery(0)
		opts = append(opts, &recovery)
	}
	for _, opt := range opts {
		if err := s.SetTransportProtocolOption(tcp.ProtocolNumber, opt); err != nil {
			return fail("tcp option", err)
		}
	}
	fwd := tcp.NewForwarder(s, forwarderWnd, forwarderMax, handler)
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, fwd.HandlePacket)
	return s, nil
}
