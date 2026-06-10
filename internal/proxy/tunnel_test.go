package proxy

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestPipeForwardsBothDirectionsAndClosesWhenIdle(t *testing.T) {
	clientApp, clientSide := net.Pipe()
	upstreamSide, upstreamApp := net.Pipe()
	var bytesUp, bytesDown int64
	done := make(chan struct{})
	go func() {
		bytesUp, bytesDown = pipe(clientSide, upstreamSide, 200*time.Millisecond)
		close(done)
	}()
	go clientApp.Write([]byte("hello"))
	assertRead(t, upstreamApp, "hello")
	go upstreamApp.Write([]byte("world!"))
	assertRead(t, clientApp, "world!")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pipe did not close after the idle timeout")
	}
	if bytesUp != 5 || bytesDown != 6 {
		t.Errorf("bytesUp=%d bytesDown=%d, want 5 and 6", bytesUp, bytesDown)
	}
}

func assertRead(t *testing.T, conn net.Conn, want string) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, len(want))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != want {
		t.Fatalf("read %q, want %q", buf, want)
	}
}
