package main

import (
	"fmt"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "backend received: %s %s\n", r.Method, r.URL.Path)
		fmt.Fprintf(w, "Host: %s\n", r.Host)
		fmt.Fprintf(w, "RemoteAddr: %s\n", r.RemoteAddr)
	})
	http.ListenAndServe(":9000", mux)
}
