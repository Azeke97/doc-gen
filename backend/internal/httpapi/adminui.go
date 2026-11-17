package httpapi

import (
	"net/http"
	"os"
	"strings"
)

// adminUICSP — что разрешено странице админки: только свои скрипты и стили (Element Plus
// ставит стили inline), PDF предпросмотра — из blob: во фрейме. frame-ancestors 'self', а не
// 'none': blob-документ наследует политику создателя и с 'none' не открылся бы даже у себя.
const adminUICSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; frame-src blob:; object-src blob:; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'self'"

// AdminUI раздаёт собранную админку из каталога dir. index.html не кэшируется,
// чтобы после обновления браузер сразу взял новые файлы; у остальных в имени хэш.
func AdminUI(dir string) http.Handler {
	files := http.FileServerFS(os.DirFS(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", adminUICSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}
