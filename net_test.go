package net

import (
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCreateReplacedNetworkingPkgOverlayFile(t *testing.T) {
	f, err := CreateReplacedNetworkingPkgOverlayFile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if f.Path() == "" {
		t.Fatalf("failed to get overlay file path")
	}
}

func TestCreateReplacedNetSource(t *testing.T) {
	tests := map[string]struct {
		source   string
		expected string
	}{
		"dialContext": {
			source: `
package net

import (
	"context"
	"time"
)

// DialContext connects to the address on the named network using
// the provided context.
func DialContext(ctx context.Context, network, address string) (Conn, error) {
	d := &Dialer{Timeout: 30 * time.Second}
	return d.DialContext(ctx, network, address)
}

type Dialer struct {
	Timeout time.Duration
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error) {
	// implementation here
	return nil, nil
}

type Conn interface{}
`,
			expected: `package net

import (
	"context"
	"time"
	_ "unsafe"
)

func DialContext(ctx context.Context, network, address string) (Conn, error) {
	d := &Dialer{Timeout: 30 * time.Second}
	return d.DialContext(ctx, network, address)
}

type Dialer struct {
	Timeout time.Duration
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error) {
	return _dialContext(ctx, network, address)
}

type Conn interface{}

//go:linkname _dialContext github.com/goccy/wasi-go-net/wasip1.DialContext
func _dialContext(ctx context.Context, network string, address string) (Conn, error)
`,
		},
		"listen": {
			source: `
package net

import (
	"syscall"
)

// Listen announces on the local network address.
func Listen(network, address string) (Listener, error) {
	var la Addr
	switch network {
	case "tcp", "tcp4", "tcp6":
		la, _ = ResolveTCPAddr(network, address)
	}
	return ListenTCP(network, la.(*TCPAddr))
}

type Listener interface{}
type Addr interface{}
type TCPAddr struct{}

func ResolveTCPAddr(network, address string) (Addr, error) {
	return &TCPAddr{}, nil
}

func ListenTCP(network string, laddr *TCPAddr) (Listener, error) {
	// implementation here
	return nil, nil
}
`,
			expected: `package net

import (
	"syscall"
	_ "unsafe"
)

func Listen(network, address string) (Listener, error) { return _listen(network, address) }

type Listener interface{}
type Addr interface{}
type TCPAddr struct{}

func ResolveTCPAddr(network, address string) (Addr, error) {
	return &TCPAddr{}, nil
}

func ListenTCP(network string, laddr *TCPAddr) (Listener, error) {

	return nil, nil
}

//go:linkname _listen github.com/goccy/wasi-go-net/wasip1.Listen
func _listen(network string, address string) (Listener, error)
`,
		},
		"dialContext and listen": {
			source: `
package net

import (
	"context"
	"time"
	"syscall"
)

// DialContext connects to the address on the named network using
// the provided context.
func DialContext(ctx context.Context, network, address string) (Conn, error) {
	d := &Dialer{Timeout: 30 * time.Second}
	return d.DialContext(ctx, network, address)
}

type Dialer struct {
	Timeout time.Duration
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error) {
	// implementation here
	return nil, nil
}

type Conn interface{}

// Listen announces on the local network address.
func Listen(network, address string) (Listener, error) {
	var la Addr
	switch network {
	case "tcp", "tcp4", "tcp6":
		la, _ = ResolveTCPAddr(network, address)
	}
	return ListenTCP(network, la.(*TCPAddr))
}

type Listener interface{}
type Addr interface{}
type TCPAddr struct{}

func ResolveTCPAddr(network, address string) (Addr, error) {
	return &TCPAddr{}, nil
}

func ListenTCP(network string, laddr *TCPAddr) (Listener, error) {
	// implementation here
	return nil, nil
}
`,
			expected: `package net

import (
	"context"
	"syscall"
	"time"
	_ "unsafe"
)

func DialContext(ctx context.Context, network, address string) (Conn, error) {
	d := &Dialer{Timeout: 30 * time.Second}
	return d.DialContext(ctx, network, address)
}

type Dialer struct {
	Timeout time.Duration
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error) {
	return _dialContext(ctx, network, address)
}

type Conn interface{}

func Listen(network, address string) (Listener, error) { return _listen(network, address) }

type Listener interface{}
type Addr interface{}
type TCPAddr struct{}

func ResolveTCPAddr(network, address string) (Addr, error) {
	return &TCPAddr{}, nil
}

func ListenTCP(network string, laddr *TCPAddr) (Listener, error) {

	return nil, nil
}

//go:linkname _dialContext github.com/goccy/wasi-go-net/wasip1.DialContext
func _dialContext(ctx context.Context, network string, address string) (Conn, error)

//go:linkname _listen github.com/goccy/wasi-go-net/wasip1.Listen
func _listen(network string, address string) (Listener, error)
`,
		},

		"import": {
			source: `
package net

import (
	"context"
	"time"
	_ "unsafe"
)

// DialContext connects to the address on the named network using
// the provided context.
func DialContext(ctx context.Context, network, address string) (Conn, error) {
	d := &Dialer{Timeout: 30 * time.Second}
	return d.DialContext(ctx, network, address)
}

type Dialer struct {
	Timeout time.Duration
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error) {
	// implementation here
	return nil, nil
}

type Conn interface{}
`,
			expected: `package net

import (
	"context"
	"time"
	_ "unsafe"
)

func DialContext(ctx context.Context, network, address string) (Conn, error) {
	d := &Dialer{Timeout: 30 * time.Second}
	return d.DialContext(ctx, network, address)
}

type Dialer struct {
	Timeout time.Duration
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (Conn, error) {
	return _dialContext(ctx, network, address)
}

type Conn interface{}

//go:linkname _dialContext github.com/goccy/wasi-go-net/wasip1.DialContext
func _dialContext(ctx context.Context, network string, address string) (Conn, error)
`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Create a temporary file with test source code
			tmpFile, err := os.CreateTemp("", "")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(test.source); err != nil {
				t.Fatalf("failed to write source code: %v", err)
			}
			tmpFile.Close()

			replacedSrc, err := createReplacedNetSource(tmpFile.Name())
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(string(replacedSrc), test.expected); diff != "" {
				t.Errorf("(-got, +want)\n%s", diff)
			}
		})
	}
}

func TestCreateReplacedX509Source(t *testing.T) {
	tests := map[string]struct {
		source   string
		expected string
	}{
		"verify": {
			source: `
package x509

import (
	"context"
	"crypto"
	"time"
)

type Certificate struct {
	Raw []byte
}

type VerifyOptions struct {
	DNSName string
	Intermediates *CertPool
}

func (c *Certificate) Verify(opts VerifyOptions) (chains [][]*Certificate, err error) {
	// Original implementation
	return nil, nil
}
`,
			expected: `package x509

import (
	"context"
	"crypto"
	"runtime"
	"time"
	_ "unsafe"
)

type Certificate struct {
	Raw []byte
}

type VerifyOptions struct {
	DNSName       string
	Intermediates *CertPool
}

func (c *Certificate) Verify(opts VerifyOptions) (chains [][]*Certificate, err error) {
	if runtime.GOOS != "wasip1" {
		return c.verify(opts)
	}
	return c.systemVerifyWasip1(&opts)
}
func (c *Certificate) verify(opts VerifyOptions) (chains [][]*Certificate, err error) {

	return nil, nil
}
`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(test.source); err != nil {
				t.Fatalf("failed to write source code: %v", err)
			}
			tmpFile.Close()

			replacedSrc, err := createReplacedX509Source(tmpFile.Name())
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(string(replacedSrc), test.expected); diff != "" {
				t.Errorf("(-got, +want)\n%s", diff)
			}
		})
	}
}

func TestCreateRootWasip1File(t *testing.T) {
	content, err := createX509RootWasip1File()
	if err != nil {
		t.Fatal(err)
	}

	if len(content) == 0 {
		t.Fatal("createRootWasip1File should return non-empty content")
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "//go:build wasip1") {
		t.Error("should contain wasip1 build constraint")
	}
	if !strings.Contains(contentStr, "package x509") {
		t.Error("should contain x509 package declaration")
	}
	if !strings.Contains(contentStr, "systemVerifyWasip1") {
		t.Error("should contain systemVerify method")
	}
}
