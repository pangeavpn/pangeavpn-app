//go:build ios && cgo

package mobile

// #include <netdb.h>
// #include <sys/socket.h>
import "C"

import "unsafe"

// nat64Synthesize returns the first IPv6 address getaddrinfo gives for literal,
// which iOS synthesizes on a NAT64 network, or literal when there is none.
func nat64Synthesize(literal string) string {
	node := append([]byte(literal), 0)
	hints := C.struct_addrinfo{ai_flags: C.AI_DEFAULT, ai_family: C.AF_UNSPEC, ai_socktype: C.SOCK_STREAM}
	var res *C.struct_addrinfo
	if C.getaddrinfo((*C.char)(unsafe.Pointer(&node[0])), nil, &hints, &res) != 0 {
		return literal
	}
	defer C.freeaddrinfo(res)
	for ai := res; ai != nil; ai = ai.ai_next {
		if ai.ai_family != C.AF_INET6 || ai.ai_addr == nil {
			continue
		}
		host := make([]byte, C.NI_MAXHOST)
		if C.getnameinfo(ai.ai_addr, ai.ai_addrlen, (*C.char)(unsafe.Pointer(&host[0])), C.socklen_t(len(host)),
			nil, 0, C.NI_NUMERICHOST) != 0 {
			return literal
		}
		return C.GoString((*C.char)(unsafe.Pointer(&host[0])))
	}
	return literal
}
