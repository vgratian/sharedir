package main

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os"
	fp "path/filepath"
	"strings"
	"time"
)

const (
	logfp = "httpfs.log"

	ic_dir	= ""
	ic_sdr	= ""
	ic_arx  = ""
	ic_aud	= ""
	ic_cod	= ""
	ic_vid	= ""
	ic_img	= ""
	ic_fil	= ""
	ic_txt	= ""

	txtHead = `
<html>
  <head>
    <title>sharedir: %s</title>
	<style type="text/css">
	table th{ text-align: justify; }
	table td{ text-align: justify; }
	</style>
  </head>
  <body>
  <h2 style="color:grey;">index: %s</h2>
  <table width="80%" style="color:grey;">
    <thead>
      <th>Filename</th>
      <th>Size</th>
      <th>Modified</th>
    </thead>
    <tbody>
`
	txtTail = `    </tbody>
  </table>
  </body>
</html>
`
	txtFileEntry = `      <tr><td><a href="%s">%s</a></td><td>%d</td><td>%s</td></tr>\n
`
	txtDirEntry = `      <tr><td><a href="%s">%s/</a></td><td>-*-</td><td>%s</td></tr>\n
`
)

var (
	rootDir = ""
	recursive = false
)

//func getIcon(fn string) string { }

func getMimeType(fn string) string {
	var (
		mtype string
		parts []string
	)

	if parts = strings.Split(fn, "."); len(parts) > 1 {
		mtype = mime.TypeByExtension("."+parts[len(parts)-1])
	}

	if mtype == "" {
		return "application/octet-stream"
	}
	return mtype
}

func serveReject(w http.ResponseWriter, s int, msg string) {
	w.WriteHeader(s)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(msg))
}

func serve(w http.ResponseWriter, r *http.Request) {
	var (
		target string
		err error
		info os.FileInfo
	)

	log.Printf("%s - %s - %s", r.Method, r.RemoteAddr, r.RequestURI)

	if target, err = fp.Abs(fp.Join(rootDir, r.RequestURI)); err != nil {
		log.Printf("error parsing path: %s", err.Error())
		serveReject(w, http.StatusBadRequest, "bad target path")
		return
	}

	if ! strings.HasPrefix(target, rootDir) {
		log.Printf("target not in root: %s", target)
		serveReject(w, http.StatusUnauthorized, "unauthorized path")
		return
	}


	// TODO handle symlinks and other non-regular files
	if info, err = os.Stat(target); err != nil {
		log.Printf("error stat target: %s", err.Error())
		serveReject(w, http.StatusNotFound, "target not found")
		return
	}

	if ! info.IsDir() {
		serveFile(w, target)
		return
	}

	serveDir(w, target, r.RequestURI)
}

func serveFile(w http.ResponseWriter, fp string) {

	var (
		err error
		num int
		data []byte
		mtype string
	)

	mtype = getMimeType(fp)

	if data, err = ioutil.ReadFile(fp); err != nil {
		log.Printf("error read [%s]: %v", fp, err)
		serveReject(w, http.StatusInternalServerError, "error")
		return
	}

	if num, err = w.Write(data); err != nil {
		log.Printf("error write response: %v", err)
		serveReject(w, http.StatusInternalServerError, "error")
		return
	}
	log.Printf("wrote data [%s]: %d", mtype, num)

	//w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", mtype) //; charset=binary")
}

func serveDir(w http.ResponseWriter, dir, subdir string) {

	var (
		err		error
		entries	[]os.DirEntry
		e		os.DirEntry
		info	fs.FileInfo
		res	string
	)

	if entries, err = os.ReadDir(dir); err != nil {
		log.Fatalf("read fsDir: %s", err.Error())
	}

	res += fmt.Sprintf(txtHead, subdir, subdir)
	for _, e = range entries {
		if info, err = e.Info(); err != nil {
			log.Printf("error: read [%s] info: %s", e.Name(), err.Error())
			continue
		}
		//s += fmt.Sprintf(txtEntry, getIcon(f), f.Name(), i.Size(), i.ModTime().String())
		if info.IsDir() {
			res += fmt.Sprintf(txtDirEntry,
					fp.Join(subdir, e.Name()),
					e.Name(),
					info.ModTime().Format(time.UnixDate),
			)
		} else {
			res += fmt.Sprintf(txtFileEntry,
					fp.Join(subdir, e.Name()),
					e.Name(), 
					info.Size(), 
					info.ModTime().Format(time.UnixDate),
			)
		}
	}
	res += txtTail

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/html; charset=utf-8") 

	if _, err = w.Write([]byte(res)); err != nil {
		log.Fatalf("write response: %s", err.Error())
	}
}


func usage() {
	fmt.Printf("usage: %s [addr] [directory] [-r]\n", fp.Base(os.Args[0]))
}


func main() {

	var (
		mux  *http.ServeMux
		srv  http.Server
		err	 error
		addr string
		mode string
	)
	
	mode = "non-recursive"
	if len(os.Args) > 1 {
		for _, a := range os.Args[1:] {

			if a == "help" || a == "--help" || a == "-h" {
				usage()
				os.Exit(0)
			}

			if a == "-r" {
				recursive = true
				mode = "recursive"
			} else if addr == "" {
				addr = a
			} else {
				rootDir = a
			}
		}
	}

	if addr != "" && rootDir == "" {
		rootDir = addr
	}

	// defaults
	if addr == "" {
		addr = ":2022"
	}

	if rootDir == "" {
		rootDir = "."
	}

	// convert to absolute path (helps to make sure we don't share anything outside)
	if rootDir, err = fp.Abs(rootDir); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	log.Printf("sharing directory %sly [%s]", mode, rootDir)

	mux = http.NewServeMux()
	mux.HandleFunc("/", serve)
	srv.Handler = mux
	srv.Addr = addr

	log.Printf("serving at [%s]", srv.Addr)
	if err = srv.ListenAndServe(); err != nil {
		log.Fatalf("start http service: %s", err.Error())
	}
}
