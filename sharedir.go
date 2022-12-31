package main

import (
	"fmt"
	"html"
	"html/template"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const templateFp = "template.html"

var (
	root      string         // root of shared directory
	recursive bool   = false // recursive mode
	home      string         // home directory of program
)

type safePath struct {
	abs string // absolute path (unvisible to clients)
	rel string // path relative to root (visible)
}

// Check if the requested path is admissible. If so, return
// a safePath instance. Path is admissible if it is
// valid and a subpath of root.
// TODO handle symlinks and non-regular files.
func parseSafePath(raw string) *safePath {
	var (
		sp  *safePath
		err error
	)

	raw = strings.TrimPrefix(raw, "/")
	raw = html.UnescapeString(raw)
	raw = strings.ReplaceAll(raw, "%20", " ")
	raw = strings.ReplaceAll(raw, "%28", "(")
	raw = strings.ReplaceAll(raw, "%29", ")")

	sp = new(safePath)
	sp.abs = filepath.Join(root, raw)

	if sp.abs, err = filepath.Abs(sp.abs); err != nil {
		log.Printf("     absolute path: %v", err)
		return nil
	}

	if !strings.HasPrefix(sp.abs, root) {
		log.Print("     not in root")
		return nil
	}

	if sp.abs != root {
		sp.rel = strings.TrimPrefix(sp.abs, root)
		sp.rel = strings.TrimPrefix(sp.rel, "/")
	}

	return sp
}

// Guess MIME-type of file based on file extension.
// By default assume 'binary data'.
func guessMimeType(fp string) string {
	var mimet, ext string

	if ext = filepath.Ext(fp); ext != "" {
		mimet = mime.TypeByExtension(ext)

		if mimet != "" {
			return mimet
		}
	}

	return "application/octet-stream"
}

// Main entry point of the HTTP handling pipeline.
// General logic: client requests a path (file or directory).
// We check if request is admissable and pass the request on
// to the other functions.
func serve(w http.ResponseWriter, r *http.Request) {
	var (
		sp  *safePath
		err error
		inf os.FileInfo
	)

	log.Printf("%s: %s - %s", r.Method, r.RemoteAddr, r.RequestURI)

	if r.RequestURI == "/~favicon.ico" {
		serveIcon(w)
		return
	}

	if sp = parseSafePath(r.RequestURI); sp == nil {
		serveFailure(w, http.StatusBadRequest, "invalid path")
		return
	}

	if inf, err = os.Stat(sp.abs); err != nil {
		log.Printf("     stat target: %v", err)
		serveFailure(w, http.StatusNotFound, "invalid path")
		return
	}

	if inf.IsDir() {
		if recursive || sp.abs == root {
			serveDir(w, sp)
			return
		}
	} else {
		if recursive || filepath.Dir(sp.abs) == root {
			serveFile(w, sp)
			return
		}
	}

	serveFailure(w, http.StatusUnauthorized, "unauthorized")
}

// Write HTTP-status-code indicating failure and the plain-text
// error message.
func serveFailure(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(message))
}

func serveFile(w http.ResponseWriter, p *safePath) {

	var (
		err  error
		size int
		data []byte
	)

	if data, err = os.ReadFile(p.abs); err != nil {
		log.Printf("     read file [%s]: %v", p.abs, err)
		serveFailure(w, http.StatusInternalServerError, "server error")
		return
	}

	if size, err = w.Write(data); err != nil {
		log.Printf("     write response: %v", err)
		serveFailure(w, http.StatusInternalServerError, "server error")
		return
	}

	w.Header().Set("Content-Type", guessMimeType(p.rel))
	log.Printf("     served %d bytes", size)
}

func serveIcon(w http.ResponseWriter) {
	var p *safePath

	p = new(safePath)
	p.abs = filepath.Join(home, "sharedir.ico")
	serveFile(w, p)
}

func serveDir(w http.ResponseWriter, p *safePath) {

	var (
		err error
		tmp *template.Template
	)

	data := struct {
		DirName string
		Content []os.DirEntry
	}{DirName: "/" + p.rel}

	if data.Content, err = os.ReadDir(p.abs); err != nil {
		log.Fatalf("read dir: %v", err)
	}

	tmp, err = template.New(templateFp).Funcs(
		template.FuncMap{
			"ttos": func(t time.Time) string {
				return t.Format("2006-01-02 15:04:05")
			},
			"href": func(n string) string {
				if p.rel == "" {
					return n
				}
				return filepath.Join(p.rel, n)
			},
		}).ParseFiles(filepath.Join(home, templateFp))

	if err != nil {
		log.Fatalf("parse template: %v", err)
	}

	if err = tmp.Execute(w, data); err != nil {
		log.Fatalf("execute template: %v", err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

const usage = `Quickly and safely share content of a directory over HTTP.

Usage: sharedir [-r] [-a ADDR] [directory]

Options and arguments:
    -r          Recursive mode (also share subdirectories)
    -a ADDR     Start HTTP server on this address (default: ':2022')
    directory   Directory to share (default: current directory)
	
Report bugs: https://github.com/vgratian/sharedir
`

func main() {

	var (
		mux  *http.ServeMux
		srv  http.Server
		err  error
		addr string
	)

	addr = ":2022"

	if len(os.Args) > 1 {
		a := os.Args[1]
		if a == "help" || a == "--help" || a == "-h" {
			fmt.Println(usage)
			os.Exit(0)
		}

		i := 1
		for i < len(os.Args) {
			a = os.Args[i]
			if a == "-r" {
				recursive = true
				i += 1
				continue
			}

			if a == "-a" {
				if i+1 < len(os.Args) {
					addr = os.Args[i+1]
					i += 2
				} else {
					fmt.Printf("missing argument for '-a'")
					os.Exit(1)
				}
				continue
			}

			root = os.Args[i]
			i += 1
		}
	}

	// convert to absolute path (helps to make sure we don't share anything outside)
	if root, err = filepath.Abs(root); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if recursive {
		log.Printf("sharing directory [%s] recursively", root)
	} else {
		log.Printf("sharing directory [%s]", root)
	}

	// home directory (where we can load html template and icon from)
	if home, err = os.Executable(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if home, err = filepath.EvalSymlinks(home); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	home, _ = filepath.Split(home)
	log.Printf("found home directory [%s]", home)

	mux = http.NewServeMux()
	mux.HandleFunc("/", serve)
	//handler := http.FileServer(http.Dir(root))
	//mux.Handle("/", handler)
	srv.Handler = mux
	srv.Addr = addr

	log.Printf("serving at %s", srv.Addr)
	if err = srv.ListenAndServe(); err != nil {
		log.Fatalf("starting HTTP service: %s", err.Error())
	}
}
