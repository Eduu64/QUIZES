package main

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed index.html
var static embed.FS

var (
	root = env("DATA_DIR", "/data")
	pass = os.Getenv("ADMIN_PASS") // opcional: si se define, pide contraseña (usuario: cualquiera)
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// Convierte una ruta del cliente en una ruta segura dentro de root.
func safe(p string) string {
	return filepath.Join(root, filepath.Clean("/"+p))
}

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
		n := i + 1
		if x.Q == "" || len(x.Options) < 2 {
			return "pregunta " + itoa(n) + ": falta 'q' o hay menos de 2 'options'"
		}
		var one int
		var many []int
		if json.Unmarshal(x.Answer, &one) == nil {
			many = []int{one}
		} else if json.Unmarshal(x.Answer, &many) != nil || len(many) == 0 {
			return "pregunta " + itoa(n) + ": 'answer' debe ser un número o lista de números"
		}
		for _, a := range many {
			if a < 0 || a >= len(x.Options) {
				return "pregunta " + itoa(n) + ": 'answer' fuera de rango"
			}
		}
	}
	return ""
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

type Entry struct {
	Path  string `json:"path"`
	Dir   bool   `json:"dir"`
	Title string `json:"title,omitempty"`
	N     int    `json:"n,omitempty"`
}

func tree(w http.ResponseWriter, _ *http.Request) {
	out := []Entry{}
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			out = append(out, Entry{Path: rel, Dir: true})
		} else if strings.HasSuffix(rel, ".json") {
			e := Entry{Path: rel}
			if b, err := os.ReadFile(p); err == nil {
				var q Quiz
				if json.Unmarshal(b, &q) == nil {
					e.Title, e.N = q.Title, len(q.Questions)
				}
			}
			out = append(out, e)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path) })
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
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
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			http.Error(w, "archivo demasiado grande", 413)
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
		os.Remove(f)
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
		os.MkdirAll(f, 0o755)
	case "DELETE":
		os.RemoveAll(f)
	}
	w.WriteHeader(204)
}

func move(w http.ResponseWriter, r *http.Request) {
	from, to := safe(r.URL.Query().Get("from")), safe(r.URL.Query().Get("to"))
	if from == root || to == root {
		http.Error(w, "ruta inválida", 400)
		return
	}
	if _, err := os.Stat(to); err == nil {
		http.Error(w, "ya existe el destino", 409)
		return
	}
	os.MkdirAll(filepath.Dir(to), 0o755)
	if os.Rename(from, to) != nil {
		http.Error(w, "no se pudo mover", 500)
		return
	}
	w.WriteHeader(204)
}

func auth(next http.Handler) http.Handler {
	if pass == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, p, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(p), []byte(pass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="quizbox"`)
			http.Error(w, "no autorizado", 401)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	os.MkdirAll(root, 0o755)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tree", tree)
	mux.HandleFunc("/api/quiz", quiz)
	mux.HandleFunc("/api/folder", folder)
	mux.HandleFunc("/api/move", move)
	mux.Handle("/", http.FileServerFS(static))
	addr := env("ADDR", ":8080")
	log.Println("quizbox escuchando en", addr, "datos en", root)
	log.Fatal(http.ListenAndServe(addr, auth(mux)))
}
