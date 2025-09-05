package wasip1

import (
	"encoding/json"
	"errors"
	"sync"
	"unsafe"

	"github.com/goccy/wasi-go/net"
)

var (
	heap   = make(map[uint32][]byte)
	heapMu sync.Mutex
)

//go:wasmexport wasip1_alloc
func wasip1_alloc(size uint32) uint32 {
	if size == 0 {
		return 0
	}
	buf := make([]byte, size)
	p := uint32(uintptr(unsafe.Pointer(&buf[0])))
	heapMu.Lock()
	heap[p] = buf
	heapMu.Unlock()
	return p
}

func wasip1_free(p uint32) {
	heapMu.Lock()
	defer heapMu.Unlock()

	delete(heap, p)
}

//go:wasmimport wasi_go_net verify_certification
func verify_certification(uint32, uint32, uint32, uint32)

func VerifyCertification(chain [][]byte, serverName string) error {
	b, err := json.Marshal(&net.VerifyOptions{
		Chain:   chain,
		DNSName: serverName,
	})
	if err != nil {
		return err
	}
	out := make([]uint32, 2)
	outp := uint32(uintptr(unsafe.Pointer(&out[0])))
	verify_certification(
		uint32(uintptr(unsafe.Pointer(&b[0]))),
		uint32(len(b)),
		outp,
		outp+4,
	)
	if out[0] == 0 {
		return nil
	}
	e := unsafe.String((*byte)(unsafe.Pointer(uintptr(out[0]))), out[1])
	msg := make([]byte, out[1])
	copy(msg, []byte(e))
	wasip1_free(out[0])
	return errors.New(string(msg))
}
