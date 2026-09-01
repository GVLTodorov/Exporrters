package main

import (
	"bufio"
	"io"
	"log"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-routeros/routeros/v3/proto"
	"go.uber.org/goleak"
)

// TestMain runs a whole-package goroutine-leak check after every test
// finishes, so any test that opens something (a connection, a timer) and
// forgets to release it fails loudly instead of silently accumulating
// background goroutines in a long-running process.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// startFakeRouterOSServer accepts connections and speaks just enough of the
// RouterOS API wire protocol (via the same proto package the real client
// uses) to complete a login and ack every command with "!done", optionally
// adding "=ret=1" when the command included the count-only flag. It exists
// so TestCollectOnceDoesNotLeakConnections can drive real net.Conn/TCP dial
// and Close() calls instead of a stub, which is what a resource leak in
// client.dial()/collectOnce would actually manifest as.
//
// It also returns a live counter of currently-open connections. A missing
// Close() on the client side shows up there as a count that never returns
// to zero — a check on it doesn't depend on Go's GC ever running the
// abandoned socket's finalizer, which (confirmed by briefly disabling
// collectOnce's Close() while writing this test) can otherwise silently
// paper over exactly this kind of bug within a short-lived test process.
func startFakeRouterOSServer(t *testing.T) (addr string, active *atomic.Int64) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	active = new(atomic.Int64)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			active.Add(1)
			go serveFakeRouterOSConn(conn, active)
		}
	}()
	return ln.Addr().String(), active
}

func serveFakeRouterOSConn(conn net.Conn, active *atomic.Int64) {
	defer active.Add(-1)
	defer conn.Close()
	br := bufio.NewReader(conn)
	w := proto.NewWriter(conn)
	for {
		words, err := readSentenceWords(br)
		if err != nil {
			return
		}
		w.BeginSentence()
		w.WriteWord("!done")
		for _, word := range words {
			if word == "=count-only=" {
				w.WriteWord("=ret=1")
			}
		}
		if err := w.EndSentence(); err != nil {
			return
		}
	}
}

// readSentenceWords reads one RouterOS API sentence: a stream of
// length-prefixed words terminated by a zero-length word. This mirrors the
// (unexported) decoding in github.com/go-routeros/routeros/v3/proto, which
// can't be reused directly here because that Reader is written for parsing
// server *replies* and rejects the "?filter" query words a real client
// command sends. Only the one- and two-byte length forms are implemented
// (words up to 16383 bytes) since every sentence this exporter ever sends is
// a short command/filter word, well under that.
func readSentenceWords(br *bufio.Reader) ([]string, error) {
	var words []string
	for {
		l, err := readWordLength(br)
		if err != nil {
			return nil, err
		}
		if l == 0 {
			return words, nil
		}
		buf := make([]byte, l)
		if _, err := io.ReadFull(br, buf); err != nil {
			return nil, err
		}
		words = append(words, string(buf))
	}
}

func readWordLength(br *bufio.Reader) (int, error) {
	b0, err := br.ReadByte()
	if err != nil {
		return 0, err
	}
	if b0&0x80 == 0x00 {
		return int(b0), nil
	}
	b1, err := br.ReadByte()
	if err != nil {
		return 0, err
	}
	return int(b0&0x3F)<<8 | int(b1), nil
}

// TestCollectOnceDoesNotLeakConnections drives collectOnce's real
// dial-run-Close lifecycle (not the stub runner metrics_test.go uses)
// through many cycles against an in-process fake router. It checks two
// things: the fake server's open-connection count returns to zero promptly
// (a deterministic signal a missing Close() would break immediately), and
// goleak finds no goroutine left running (a broader safety net for leaks
// that aren't tied to the connection itself, e.g. a stray timer).
func TestCollectOnceDoesNotLeakConnections(t *testing.T) {
	// The fake server acks every command generically, so fetches that aren't
	// count-only (system resource, interfaces) come back "empty" every
	// cycle; collectOnce logs that expected noise, which isn't relevant here.
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	addr, active := startFakeRouterOSServer(t)

	// Baseline captured after the server's accept loop is already running,
	// so only goroutines collectOnce itself leaves behind get flagged below.
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}

	client := newClient(Config{
		Address:        host,
		APIPort:        port,
		User:           "prometheus",
		Password:       "secret",
		RequestTimeout: 5 * time.Second,
	})

	const cycles = 200
	for i := 0; i < cycles; i++ {
		collectOnce(client)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if n := active.Load(); n == 0 {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("%d connection(s) still open %v after the last collectOnce call — Close() is not releasing them", n, 2*time.Second)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
