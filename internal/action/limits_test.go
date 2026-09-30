package action

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

// A batch at its ceiling with a copy for the feed beside every file still
// fits in one request of the socket.
func TestSocketCarriesBiggestPackWithCopies(t *testing.T) {
	got := make(chan []File, 1)
	stand := newStand(t, ExecutorFunc(func(_ context.Context, req Request) (string, error) {
		got <- req.Files
		return "accepted", nil
	}))

	files := make([]File, 0, FilesMax)
	for i := range FilesMax {
		data := make([]byte, FilesBytesMax/FilesMax)
		preview := make([]byte, PreviewMax)
		copy(preview, jpegMark)
		for j := range data {
			data[j] = byte(i + j)
		}
		for j := len(jpegMark); j < len(preview); j++ {
			preview[j] = byte(i * j)
		}
		files = append(files, File{Name: fmt.Sprintf("IMG_%04d.HEIC", i), Data: data, Preview: preview})
	}
	req := Request{ID: "a1", Kind: SessionFile, Target: "aacpanel", Files: files}
	resp, err := stand.client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("a batch with copies at the ceiling does not pass through the socket: %v", err)
	}
	if !resp.OK {
		t.Fatalf("the executor refused: %s", resp.Error)
	}

	select {
	case arrived := <-got:
		if len(arrived) != len(files) {
			t.Fatalf("%d attachments reached the executor, expected %d", len(arrived), len(files))
		}
		for i, f := range arrived {
			if !bytes.Equal(f.Data, files[i].Data) || !bytes.Equal(f.Preview, files[i].Preview) {
				t.Errorf("%s arrived corrupted", files[i].Name)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the executor never received the request")
	}
}

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
