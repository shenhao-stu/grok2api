package media

import (
	"bytes"
	"crypto/sha256"
	"errors"
	mediadomain "github.com/chenyme/grok2api/backend/internal/domain/media"
	"io"
	"os"
	"testing"
	"time"
)

func TestInputUpload100MiBBoundedChunksRestartAndIdempotency(t *testing.T) {
	root := t.TempDir()
	store, err := NewInputUploadStore(root)
	if err != nil {
		t.Fatal(err)
	}
	upload, err := store.Create(1, 100<<20, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.New()
	chunk := bytes.Repeat([]byte{137}, InputChunkBytes)
	for offset := int64(0); offset < upload.Size; {
		data := chunk
		if remaining := upload.Size - offset; remaining < int64(len(data)) {
			data = data[:remaining]
		}
		expected.Write(data)
		value, err := store.Append(1, upload.ID, offset, data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.Append(2, upload.ID, offset, data); !errors.Is(err, ErrInputUpload) {
			t.Fatal("cross-owner append accepted")
		}
		retry, err := store.Append(1, upload.ID, offset, data)
		if err != nil || retry.Offset != value.Offset {
			t.Fatal("retry duplicated bytes", err)
		}
		offset = value.Offset
		store, err = NewInputUploadStore(root)
		if err != nil {
			t.Fatal(err)
		}
	}
	saves := 0
	complete := func(_ string, reader io.Reader) (mediadomain.Asset, error) {
		saves++
		actual := sha256.New()
		size, err := io.Copy(actual, reader)
		if err != nil || size != 100<<20 || !bytes.Equal(actual.Sum(nil), expected.Sum(nil)) {
			t.Fatal("100 MiB changed or truncated", err)
		}
		return mediadomain.Asset{ID: "input_fixture", SizeBytes: size}, nil
	}
	for i := 0; i < 2; i++ {
		asset, err := store.Complete(1, upload.ID, complete)
		if err != nil || asset.SizeBytes != 100<<20 {
			t.Fatal(err)
		}
	}
	if saves != 1 {
		t.Fatal("completion retried storage mutation")
	}
	if _, err = store.Complete(2, upload.ID, complete); !errors.Is(err, ErrInputUpload) {
		t.Fatal("cross-owner completion accepted")
	}
	if _, err = os.Stat(store.path(upload.ID, ".bin")); !os.IsNotExist(err) {
		t.Fatal("completed chunk spool not removed")
	}
}

func TestInputUploadBoundsConflictExpiryAndPartialWrite(t *testing.T) {
	store, err := NewInputUploadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	store.now = func() time.Time { return now }
	for _, size := range []int64{-1, 0, (100 << 20) + 1} {
		if _, err = store.Create(1, size, "image/png"); !errors.Is(err, ErrInputUpload) {
			t.Fatal("accepted bad size", size)
		}
	}
	value, err := store.Create(1, 6, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Append(1, value.ID, 1, []byte("ab")); !errors.Is(err, ErrInputUploadConflict) {
		t.Fatal("gap accepted")
	}
	if _, err = store.Append(1, value.ID, 0, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Append(1, value.ID, 0, []byte("abd")); !errors.Is(err, ErrInputUploadConflict) {
		t.Fatal("changed replay accepted")
	}
	if _, err = store.Complete(1, value.ID, func(string, io.Reader) (mediadomain.Asset, error) {
		t.Fatal("incomplete finalized")
		return mediadomain.Asset{}, nil
	}); !errors.Is(err, ErrInputUploadConflict) {
		t.Fatal(err)
	}
	f, err := os.OpenFile(store.path(value.ID, ".bin"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("BAD"))
	f.Close()
	if _, err = store.Append(1, value.ID, 3, []byte("def")); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(store.path(value.ID, ".bin"))
	if string(raw) != "abcdef" {
		t.Fatal("uncommitted suffix retained")
	}
	now = now.Add(inputUploadTTL + time.Second)
	if _, err = store.Append(1, value.ID, 3, []byte("def")); !errors.Is(err, ErrInputUpload) {
		t.Fatal("expired upload accepted")
	}
	if _, err = os.Stat(store.path(value.ID, ".bin")); !os.IsNotExist(err) {
		t.Fatal("expired bytes retained")
	}
	if _, err = store.Append(1, "../../outside", 0, []byte("a")); !errors.Is(err, ErrInputUpload) {
		t.Fatal("unsafe id accepted")
	}
}
func TestInputUploadCapacityIsReservedBeforeBytesArrive(t *testing.T) {
	store, err := NewInputUploadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	store.now = func() time.Time { return now }
	for i := 0; i < inputUploadSlots; i++ {
		if _, err = store.Create(1, 100<<20, "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.Create(1, 1, "image/png"); !errors.Is(err, ErrMediaCapacity) {
		t.Fatal("unbounded sessions")
	}
	now = now.Add(inputUploadTTL + time.Second)
	if _, err = store.Create(1, 100<<20, "image/png"); err != nil {
		t.Fatal("expired reservations not collected", err)
	}
}

func TestCompletedUploadsReleaseConcurrencyButKeepByteBudget(t *testing.T) {
	store, err := NewInputUploadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	for i := 0; i < inputUploadSlots+1; i++ {
		u, err := store.Create(1, 1, "image/png")
		if err != nil {
			t.Fatal("completed session retained a concurrency slot", err)
		}
		if _, err = store.Append(1, u.ID, 0, []byte("x")); err != nil {
			t.Fatal(err)
		}
		if _, err = store.Complete(1, u.ID, func(string, io.Reader) (mediadomain.Asset, error) {
			return mediadomain.Asset{ID: "input_fixture", SizeBytes: 1}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	count, total, err := store.sweep()
	if err != nil || count != 0 || total != inputUploadSlots+1 {
		t.Fatal(count, total, err)
	}
}
