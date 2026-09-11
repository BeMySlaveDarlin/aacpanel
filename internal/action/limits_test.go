package action

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestSocketCarriesBiggestFile(t *testing.T) {
	got := make(chan []File, 1)
	stand := newStand(t, ExecutorFunc(func(_ context.Context, req Request) (string, error) {
		got <- req.Files
		return "accepted", nil
	}))

	data := make([]byte, FileMax)
	for i := range data {
		data[i] = byte(i)
	}
	req := Request{ID: "a1", Kind: SessionFile, Target: "aacpanel", Files: []File{{Name: "big.bin", Data: data}}}
	resp, err := stand.client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("a file at the size limit does not pass through the socket: %v", err)
	}
	if !resp.OK {
		t.Fatalf("the executor refused: %s", resp.Error)
	}

	select {
	case files := <-got:
		if len(files) != 1 {
			t.Fatalf("%d attachments reached the executor, expected one", len(files))
		}
		f := files[0]
		if len(f.Data) != len(data) {
			t.Fatalf("%d bytes of %d arrived", len(f.Data), len(data))
		}
		if !bytes.Equal(f.Data, data) {
			t.Error("the bytes arrived corrupted")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the executor never received the request")
	}
}
