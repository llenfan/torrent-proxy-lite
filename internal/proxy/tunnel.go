package proxy

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

func pipe(client, upstream net.Conn, idleTimeout time.Duration) (bytesUp, bytesDown int64) {
	var lastActivity atomic.Int64
	touch := func() { lastActivity.Store(time.Now().UnixNano()) }
	touch()
	done := make(chan struct{})
	go closeWhenIdle(done, &lastActivity, idleTimeout, client, upstream)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		bytesUp, _ = io.Copy(upstream, &activityReader{conn: client, touch: touch})
		closeWrite(upstream)
	}()
	go func() {
		defer wg.Done()
		bytesDown, _ = io.Copy(client, &activityReader{conn: upstream, touch: touch})
		closeWrite(client)
	}()
	wg.Wait()
	close(done)
	return bytesUp, bytesDown
}

type activityReader struct {
	conn  net.Conn
	touch func()
}

func (r *activityReader) Read(b []byte) (int, error) {
	n, err := r.conn.Read(b)
	if n > 0 {
		r.touch()
	}
	return n, err
}

func closeWhenIdle(done <-chan struct{}, lastActivity *atomic.Int64, idleTimeout time.Duration, conns ...net.Conn) {
	interval := max(idleTimeout/4, 10*time.Millisecond)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if time.Since(time.Unix(0, lastActivity.Load())) > idleTimeout {
				for _, c := range conns {
					c.Close()
				}
				return
			}
		}
	}
}

func closeWrite(conn net.Conn) {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := conn.(closeWriter); ok {
		cw.CloseWrite()
	}
}
