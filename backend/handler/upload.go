package handler

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const maxUpload = 5 << 20 // 5 МБ

var imageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Upload принимает multipart-поле "file" и возвращает {"url": "/uploads/<имя>"}.
// Тип определяется по содержимому, а не по расширению или заголовку клиента.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request, uid int64) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_upload", "Передайте картинку в поле file (до 5 МБ)")
		return
	}
	defer file.Close()
	defer r.MultipartForm.RemoveAll()

	if hdr.Size > maxUpload {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "Картинка больше 5 МБ")
		return
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	ext, ok := imageTypes[http.DetectContentType(head[:n])]
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "bad_type", "Допустимы JPEG, PNG, GIF и WebP")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		fail(w, err)
		return
	}

	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		fail(w, err)
		return
	}
	name := hex.EncodeToString(raw) + ext

	if err := os.MkdirAll(h.cfg.UploadDir, 0o755); err != nil {
		fail(w, err)
		return
	}
	dst, err := os.OpenFile(filepath.Join(h.cfg.UploadDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := io.Copy(dst, io.LimitReader(file, maxUpload)); err != nil {
		dst.Close()
		os.Remove(dst.Name())
		fail(w, err)
		return
	}
	if err := dst.Close(); err != nil {
		fail(w, err)
		return
	}
	log.Printf("upload: user=%d file=%s", uid, name)
	writeJSON(w, http.StatusCreated, map[string]string{"url": "/uploads/" + name})
}

// ServeUpload отдаёт загруженные файлы. Имена случайные и неизменяемые — кэш можно делать вечным.
func (h *Handler) ServeUpload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if filepath.Base(name) != name || name == "." || name == ".." {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(h.cfg.UploadDir, name))
}
