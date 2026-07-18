package executor

import (
	"context"
	"testing"
	"time"
)

func TestWrapWithHoldbackStress(t *testing.T) {
	for i := 0; i < 2000; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		chunks := make(chan StreamChunk, 64)
		go func() {
			defer close(chunks)
			for j := 0; j < 5000; j++ {
				select {
				case chunks <- StreamChunk{Payload: []byte("x")}:
				case <-ctx.Done():
					return
				}
			}
		}()
		out, errCh := WrapWithHoldback(ctx, chunks, 750, 64*1024)
		select {
		case <-errCh:
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for holdback commit")
		}
		// read a few then cancel
		for k := 0; k < 10; k++ {
			select {
			case <-out:
			case <-time.After(time.Second):
				goto done
			}
		}
	done:
		cancel()
		// drain to avoid leaks
		go func() {
			for range out {
			}
		}()
		time.Sleep(2 * time.Millisecond)
	}
}
