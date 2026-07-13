//go:build linux && cgo

package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"io"
	"net"
	"testing"
	"time"
)

// encodeCompact spiegelt compactLen im Server (internal/agent/remote/rfb.go), damit
// readCompactLen als Gegenstück verifiziert werden kann.
func encodeCompact(n int) []byte {
	b := []byte{byte(n & 0x7f)}
	if n > 0x7f {
		b[0] |= 0x80
		b = append(b, byte((n>>7)&0x7f))
		if n > 0x3fff {
			b[1] |= 0x80
			b = append(b, byte((n>>14)&0xff))
		}
	}
	return b
}

func TestReadCompactLen(t *testing.T) {
	// 0x3fffff = größtmögliche im Tight-Compact-Format darstellbare Länge (22 Bit).
	for _, n := range []int{0, 1, 0x7f, 0x80, 200, 0x3fff, 0x4000, 20000, 300000, 0x3fffff} {
		got, err := readCompactLen(bytes.NewReader(encodeCompact(n)))
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if got != n {
			t.Errorf("readCompactLen(%d) = %d", n, got)
		}
	}
}

func TestJpegToBGRX(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	// Pixel 0 = (R=10,G=20,B=30), Pixel 1 = (R=200,G=100,B=50)
	img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3] = 10, 20, 30, 255
	img.Pix[4], img.Pix[5], img.Pix[6], img.Pix[7] = 200, 100, 50, 255

	out := jpegToBGRX(img, 2, 1)
	want := []byte{30, 20, 10, 255, 50, 100, 200, 255} // BGRX
	if !bytes.Equal(out, want) {
		t.Errorf("jpegToBGRX = %v, want %v", out, want)
	}
}

func TestRuneToKeysym(t *testing.T) {
	cases := map[rune]uint32{
		'A':      0x41,
		'a':      0x61,
		'!':      0x21,
		'ä': 0x00e4,     // ä (Latin-1)
		'€': 0x010020ac, // € (Unicode-Keysym)
	}
	for r, want := range cases {
		if got := runeToKeysym(r); got != want {
			t.Errorf("runeToKeysym(%q) = %#x, want %#x", r, got, want)
		}
	}
}

// mockServer spiegelt exakt die Byte-Sequenz des eingebauten RFB-Servers
// (internal/agent/remote/rfb.go): Handshake + ein Raw-FramebufferUpdate.
func mockServer(t *testing.T, conn net.Conn, w, h int, firstPixel [4]byte) {
	t.Helper()
	rd := func(n int) {
		if _, err := io.ReadFull(conn, make([]byte, n)); err != nil {
			t.Errorf("server read %d: %v", n, err)
		}
	}
	be16 := func(v int) []byte { return []byte{byte(v >> 8), byte(v)} }

	conn.Write([]byte("RFB 003.008\n"))
	rd(12)                          // client version
	conn.Write([]byte{1, 1})        // security: 1 typ = None
	rd(1)                           // gewählter typ
	conn.Write([]byte{0, 0, 0, 0})  // SecurityResult
	rd(1)                           // ClientInit
	si := append(be16(w), be16(h)...)
	si = append(si, make([]byte, 16)...) // pixelformat (vom client ignoriert)
	si = append(si, 0, 0, 0, 0)          // name-länge 0
	conn.Write(si)

	// SetEncodings (type 2 + pad + 2 count + count*4) und FramebufferUpdateRequest (10).
	rd(1) // type=2
	rd(1) // pad
	cnt := make([]byte, 2)
	io.ReadFull(conn, cnt)
	rd(int(binary.BigEndian.Uint16(cnt)) * 4)
	rd(10) // FBUR

	// Ein Raw-Rechteck über den ganzen Schirm.
	frame := []byte{0, 0}         // FramebufferUpdate + pad
	frame = append(frame, be16(1)...) // 1 rechteck
	frame = append(frame, be16(0)...) // x
	frame = append(frame, be16(0)...) // y
	frame = append(frame, be16(w)...)
	frame = append(frame, be16(h)...)
	frame = append(frame, 0, 0, 0, 0) // encoding Raw(0)
	pix := make([]byte, w*h*4)
	copy(pix, firstPixel[:])
	conn.Write(frame)
	conn.Write(pix)
}

func TestClientRawFrameE2E(t *testing.T) {
	srv, cli := net.Pipe()
	defer cli.Close()
	const w, h = 64, 48
	first := [4]byte{1, 2, 3, 4} // B,G,R,X
	go func() { defer srv.Close(); mockServer(t, srv, w, h, first) }()

	rc, err := rfbHandshake(cli)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if rc.W != w || rc.H != h {
		t.Fatalf("größe %dx%d, erwartet %dx%d", rc.W, rc.H, w, h)
	}
	if err := rc.setEncodings(7, 0); err != nil {
		t.Fatal(err)
	}
	if err := rc.requestUpdate(false); err != nil {
		t.Fatal(err)
	}
	updates := make(chan rectUpdate, 4)
	go rc.readLoop(updates, make(chan string, 1))

	select {
	case up := <-updates:
		if up.w != w || up.h != h {
			t.Fatalf("rect %dx%d", up.w, up.h)
		}
		if len(up.pix) != w*h*4 || !bytes.Equal(up.pix[:4], first[:]) {
			t.Fatalf("pixel %v", up.pix[:4])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("kein frame empfangen")
	}
}

func TestWsBase(t *testing.T) {
	cases := map[string]string{
		"https://x.de":     "wss://x.de",
		"http://x.de:8080": "ws://x.de:8080",
		"wss://x.de":       "wss://x.de",
		"x.de":             "wss://x.de",
	}
	for in, want := range cases {
		if got := wsBase(in); got != want {
			t.Errorf("wsBase(%q) = %q, want %q", in, got, want)
		}
	}
}
