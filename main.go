package main

import (
        "bytes"
        "compress/gzip"
        "crypto/subtle"
        _ "embed"
        "encoding/json"
        "hash/fnv"
        "io"
        "log"
        "net/http"
        "os"
        "path/filepath"
        "strconv"
        "strings"
        "sync"
)

//go:embed index.html
var indexHTML []byte

var (
        root = env("DATA_DIR", "/data")
        pass = os.Getenv("ADMIN_PASS") // opcional: si se define, pide contraseña (usuario: cualquiera)
        gz   []byte
        etag string
)

func env(k, d string) string {
        if v := os.Getenv(k); v != "" {
                return v
        }
        return d
}

// Ruta del cliente -> ruta segura dentro de root (imposible salir con "..").
func safe(p string) string { return filepath.Join(root, filepath.Clean("/"+p)) }

type Question struct {
        Q           string          `json:"q"`
        Options     []string        `json:"options"`
        Answer      json.RawMessage `json:"answer"`
        Explanation string          `json:"explanation,omitempty"`
}
type Quiz struct {
        Title     string     `json:"title"`
        Questions []Question `json:"questions"`
}

func validate(q *Quiz) string {
        if strings.TrimSpace(q.Title) == "" {
                return "falta 'title'"
        }
        if len(q.Questions) == 0 {
                return "no hay preguntas"
        }
        for i, x := range q.Questions {
                n := strconv.Itoa(i + 1)
                if x.Q == "" || len(x.Options) < 2 {
                        return "pregunta " + n + ": falta 'q' o hay menos de 2 'options'"
                }
                var one int
                var many []int
                if json.Unmarshal(x.Answer, &one) == nil {
                        many = []int{one}
                } else if json.Unmarshal(x.Answer, &many) != nil || len(many) == 0 {
                        return "pregunta " + n + ": 'answer' debe ser un número o una lista de números"
                }
                for _, a := range many {
                        if a < 0 || a >= len(x.Options) {
                                return "pregunta " + n + ": 'answer' fuera de rango"
                        }
                }
        }
        return ""
}

type Entry struct {
        Path  string `json:"path"`
        Dir   bool   `json:"dir"`
        Title string `json:"title,omitempty"`
        N     int    `json:"n,omitempty"`
}

// Caché de título/nº de preguntas por archivo (solo relee si cambia tamaño o fecha).
type meta struct {
        mod, size int64
        title     string
        n         int
}

var (
        mu    sync.Mutex
        cache = map[string]meta{}
)

func tree(w http.ResponseWriter, _ *http.Request) {
        mu.Lock()
        defer mu.Unlock()
        out := []Entry{}
        seen := map[string]meta{}
        filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
                if err != nil || p == root {
                        return nil
                }
                rel, _ := filepath.Rel(root, p)
                rel = filepath.ToSlash(rel)
                if d.IsDir() {
                        out = append(out, Entry{Path: rel, Dir: true})
                        return nil
                }
                if !strings.HasSuffix(rel, ".json") {
                        return nil
                }
                fi, err := d.Info()
                if err != nil {
                        return nil
                }
                m, ok := cache[rel]
                if !ok || m.mod != fi.ModTime().UnixNano() || m.size != fi.Size() {
                        m = meta{mod: fi.ModTime().UnixNano(), size: fi.Size()}
                        if b, err := os.ReadFile(p); err == nil {
                                var q struct {
                                        Title     string
                                        Questions []json.RawMessage
                                }
                                if json.Unmarshal(b, &q) == nil {
                                        m.title, m.n = q.Title, len(q.Questions)
                                }
                        }
                }
                seen[rel] = m
                out = append(out, Entry{Path: rel, Title: m.title, N: m.n})
                return nil
        })
        cache = seen
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(out)
}

func quiz(w http.ResponseWriter, r *http.Request) {
        p := r.URL.Query().Get("path")
        if !strings.HasSuffix(p, ".json") {
                http.Error(w, "la ruta debe acabar en .json", 400)
                return
        }
        f := safe(p)
        switch r.Method {
        case "GET":
                http.ServeFile(w, r, f)
        case "PUT":
                body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
                if err != nil {
                        http.Error(w, "archivo demasiado grande (máx. 4 MB)", 413)
                        return
                }
                var q Quiz
                if err := json.Unmarshal(body, &q); err != nil {
                        http.Error(w, "JSON inválido: "+err.Error(), 400)
                        return
                }
                if msg := validate(&q); msg != "" {
                        http.Error(w, msg, 400)
                        return
                }
                os.MkdirAll(filepath.Dir(f), 0o755)
                tmp := f + ".tmp"
                if err := os.WriteFile(tmp, body, 0o644); err != nil || os.Rename(tmp, f) != nil {
                        http.Error(w, "no se pudo guardar", 500)
                        return
                }
                w.WriteHeader(204)
        case "DELETE":
                if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
                        http.Error(w, "no se pudo borrar: "+err.Error(), 500)
                        return
                }
                w.WriteHeader(204)
        default:
                http.Error(w, "método no permitido", 405)
        }
}

func folder(w http.ResponseWriter, r *http.Request) {
        f := safe(r.URL.Query().Get("path"))
        if f == root {
                http.Error(w, "ruta inválida", 400)
                return
        }
        switch r.Method {
        case "POST":
                if err := os.MkdirAll(f, 0o755); err != nil {
                        http.Error(w, "no se pudo crear la carpeta: "+err.Error(), 500)
                        return
                }
        case "DELETE":
                if err := os.RemoveAll(f); err != nil {
                        http.Error(w, "no se pudo borrar la carpeta: "+err.Error(), 500)
                        return
                }
        }
        w.WriteHeader(204)
}

func move(w http.ResponseWriter, r *http.Request) {
        from, to := safe(r.URL.Query().Get("from")), safe(r.URL.Query().Get("to"))
        if from == root || to == root || strings.HasPrefix(to, from+string(filepath.Separator)) {
                http.Error(w, "movimiento no válido", 400)
                return
        }
        if _, err := os.Stat(to); err == nil {
                http.Error(w, "ya existe algo con ese nombre en el destino", 409)
                return
        }
        os.MkdirAll(filepath.Dir(to), 0o755)
        if err := os.Rename(from, to); err != nil {
                http.Error(w, "no se pudo mover: "+err.Error(), 500)
                return
        }
        w.WriteHeader(204)
}

// La interfaz se comprime una sola vez al arrancar y se sirve desde memoria.
func initIndex() {
        var b bytes.Buffer
        z, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
        z.Write(indexHTML)
        z.Close()
        gz = b.Bytes()
        h := fnv.New64a()
        h.Write(indexHTML)
        etag = `"` + strconv.FormatUint(h.Sum64(), 36) + `"`
}

func index(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/" {
                http.NotFound(w, r)
                return
        }
        h := w.Header()
        h.Set("ETag", etag)
        h.Set("Cache-Control", "no-cache")
        h.Set("Content-Type", "text/html; charset=utf-8")
        h.Set("Vary", "Accept-Encoding")
        if r.Header.Get("If-None-Match") == etag {
                w.WriteHeader(304)
                return
        }
        if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
                h.Set("Content-Encoding", "gzip")
                w.Write(gz)
                return
        }
        w.Write(indexHTML)
}

func wrap(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if pass != "" {
                        _, p, ok := r.BasicAuth()
                        if !ok || subtle.ConstantTimeCompare([]byte(p), []byte(pass)) != 1 {
                                w.Header().Set("WWW-Authenticate", `Basic realm="quizbox"`)
                                http.Error(w, "no autorizado", 401)
                                return
                        }
                }
                h := w.Header()
                h.Set("X-Content-Type-Options", "nosniff")
                h.Set("Referrer-Policy", "no-referrer")
                h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
                if strings.HasPrefix(r.URL.Path, "/api/") {
                        h.Set("Cache-Control", "no-cache")
                }
                next.ServeHTTP(w, r)
        })
}

func main() {
        os.MkdirAll(root, 0o755)
        initIndex()
        mux := http.NewServeMux()
        mux.HandleFunc("/", index)
        mux.HandleFunc("/api/tree", tree)
        mux.HandleFunc("/api/quiz", quiz)
        mux.HandleFunc("/api/folder", folder)
        mux.HandleFunc("/api/move", move)
        addr := env("ADDR", ":8080")
        log.Println("quizbox escuchando en", addr, "datos en", root)
        log.Fatal(http.ListenAndServe(addr, wrap(mux)))
}
