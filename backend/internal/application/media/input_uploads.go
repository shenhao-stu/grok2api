package media

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	mediadomain "github.com/chenyme/grok2api/backend/internal/domain/media"
)

const InputChunkBytes = 8 << 20
const inputUploadTTL = time.Hour
const inputUploadSlots = 20
const inputUploadBudget = 2 << 30

var ErrInputUpload = errors.New("invalid or expired input upload")
var ErrInputUploadConflict = errors.New("input upload offset or content differs")
var uploadIDPattern = regexp.MustCompile(`^[0-9a-f]{48}$`)

// A single process owns this directory. Metadata commits after the bytes are synced;
// a restart truncates any uncommitted suffix before accepting another chunk.
// Completed sessions release concurrency but retain their byte reservation until
// expiry. The media service independently enforces its total storage capacity.
type InputUploadStore struct {
	mu      sync.Mutex
	root    string
	now     func() time.Time
	cleanup *time.Timer
}

type InputUpload struct {
	ID        string             `json:"uploadId"`
	Owner     uint64             `json:"owner"`
	Size      int64              `json:"sizeBytes"`
	MIME      string             `json:"mimeType"`
	Offset    int64              `json:"offset"`
	ExpiresAt time.Time          `json:"expiresAt"`
	Asset     *mediadomain.Asset `json:"asset,omitempty"`
}

func NewInputUploadStore(root string) (*InputUploadStore, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	s := &InputUploadStore{root: absolute, now: time.Now}
	s.scheduleCleanup()
	return s, nil
}

// One timer per store, independent of create/delete request volume.
func (s *InputUploadStore) scheduleCleanup() {
	if s.cleanup != nil {
		return
	}
	s.cleanup = time.AfterFunc(time.Minute, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cleanup = nil
		_, _, _ = s.sweep()
		files, err := os.ReadDir(s.root)
		if err == nil && len(files) > 0 {
			s.scheduleCleanup()
		}
	})
}

func (s *InputUploadStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleanup != nil {
		s.cleanup.Stop()
		s.cleanup = nil
	}
}

func (s *InputUploadStore) path(id, extension string) string {
	return filepath.Join(s.root, id+extension)
}
func (s *InputUploadStore) read(id string) (InputUpload, error) {
	var upload InputUpload
	if !uploadIDPattern.MatchString(id) {
		return upload, ErrInputUpload
	}
	file, err := os.Open(s.path(id, ".json"))
	if err != nil {
		return upload, ErrInputUpload
	}
	defer file.Close()
	if json.NewDecoder(io.LimitReader(file, 16<<10)).Decode(&upload) != nil || upload.ID != id || upload.Owner == 0 || upload.Size <= 0 || upload.Size > mediadomain.MaxInputAssetBytes || upload.Offset < 0 || upload.Offset > upload.Size {
		return InputUpload{}, ErrInputUpload
	}
	return upload, nil
}
func (s *InputUploadStore) write(upload InputUpload) error {
	raw, err := json.Marshal(upload)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(s.path(upload.ID, ".json.tmp"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(s.path(upload.ID, ".json.tmp"), s.path(upload.ID, ".json"))
}
func (s *InputUploadStore) remove(id string) error {
	for _, ext := range []string{".bin", ".json.tmp", ".json"} {
		if err := os.Remove(s.path(id, ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func (s *InputUploadStore) sweep() (int, int64, error) {
	files, err := os.ReadDir(s.root)
	if err != nil {
		return 0, 0, err
	}
	count, total, retained := 0, int64(0), 0
	for _, entry := range files {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-5]
		upload, err := s.read(id)
		if err != nil {
			return 0, 0, err
		}
		if !upload.ExpiresAt.After(s.now()) {
			if err = s.remove(id); err != nil {
				return 0, 0, err
			}
			continue
		}
		if upload.Asset == nil {
			count++
		}
		retained++
		total += upload.Size
	}
	if retained >= 10000 {
		return 0, 0, ErrMediaCapacity
	}
	return count, total, nil
}
func (s *InputUploadStore) Create(owner uint64, size int64, mime string) (InputUpload, error) {
	if owner == 0 || size <= 0 || size > mediadomain.MaxInputAssetBytes {
		return InputUpload{}, ErrInputUpload
	}
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "video/mp4", "video/webm", "video/quicktime":
	default:
		return InputUpload{}, ErrInputUpload
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	count, total, err := s.sweep()
	if err != nil {
		return InputUpload{}, err
	}
	if count >= inputUploadSlots || size > inputUploadBudget-total {
		return InputUpload{}, ErrMediaCapacity
	}
	var random [24]byte
	if _, err = rand.Read(random[:]); err != nil {
		return InputUpload{}, err
	}
	upload := InputUpload{ID: hex.EncodeToString(random[:]), Owner: owner, Size: size, MIME: mime, ExpiresAt: s.now().UTC().Add(inputUploadTTL)}
	file, err := os.OpenFile(s.path(upload.ID, ".bin"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return InputUpload{}, err
	}
	file.Close()
	if err = s.write(upload); err != nil {
		_ = s.remove(upload.ID)
		return InputUpload{}, err
	}
	s.scheduleCleanup()
	return upload, nil
}
func (s *InputUploadStore) owned(owner uint64, id string) (InputUpload, error) {
	upload, err := s.read(id)
	if err != nil || upload.Owner != owner {
		return InputUpload{}, ErrInputUpload
	}
	if !upload.ExpiresAt.After(s.now()) {
		_ = s.remove(id)
		return InputUpload{}, ErrInputUpload
	}
	return upload, nil
}
func (s *InputUploadStore) Append(owner uint64, id string, offset int64, data []byte) (InputUpload, error) {
	if len(data) == 0 || len(data) > InputChunkBytes || offset < 0 {
		return InputUpload{}, ErrInputUpload
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	upload, err := s.owned(owner, id)
	if err != nil {
		return InputUpload{}, err
	}
	if upload.Asset != nil || offset > upload.Offset || int64(len(data)) > upload.Size-offset {
		return InputUpload{}, ErrInputUploadConflict
	}
	file, err := os.OpenFile(s.path(id, ".bin"), os.O_RDWR, 0600)
	if err != nil {
		return InputUpload{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() < upload.Offset {
		return InputUpload{}, ErrInputUploadConflict
	}
	if offset < upload.Offset {
		if offset+int64(len(data)) > upload.Offset {
			return InputUpload{}, ErrInputUploadConflict
		}
		old := make([]byte, len(data))
		if _, err = file.ReadAt(old, offset); err != nil || !bytes.Equal(old, data) {
			return InputUpload{}, ErrInputUploadConflict
		}
		return upload, nil
	}
	if err = file.Truncate(upload.Offset); err != nil {
		return InputUpload{}, err
	}
	if _, err = file.WriteAt(data, offset); err != nil {
		return InputUpload{}, err
	}
	if err = file.Sync(); err != nil {
		return InputUpload{}, err
	}
	upload.Offset += int64(len(data))
	if err = s.write(upload); err != nil {
		return InputUpload{}, err
	}
	return upload, nil
}
func (s *InputUploadStore) Complete(owner uint64, id string, save func(string, io.Reader) (mediadomain.Asset, error)) (mediadomain.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	upload, err := s.owned(owner, id)
	if err != nil {
		return mediadomain.Asset{}, err
	}
	if upload.Asset != nil {
		return *upload.Asset, nil
	}
	if upload.Offset != upload.Size {
		return mediadomain.Asset{}, ErrInputUploadConflict
	}
	file, err := os.Open(s.path(id, ".bin"))
	if err != nil {
		return mediadomain.Asset{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != upload.Size {
		return mediadomain.Asset{}, ErrInputUploadConflict
	}
	asset, err := save(upload.MIME, io.LimitReader(file, upload.Size))
	if err != nil {
		return mediadomain.Asset{}, err
	}
	upload.Asset = &asset
	if err = s.write(upload); err != nil {
		return mediadomain.Asset{}, err
	}
	file.Close()
	if err = os.Remove(s.path(id, ".bin")); err != nil {
		return mediadomain.Asset{}, err
	}
	return asset, nil
}
func (s *InputUploadStore) Delete(owner uint64, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	upload, err := s.owned(owner, id)
	if err != nil {
		return err
	}
	// A completed reservation lives until expiry; deleting it would bypass the budget.
	if upload.Asset != nil {
		return nil
	}
	return s.remove(id)
}
