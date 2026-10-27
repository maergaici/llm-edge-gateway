package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "[ORDERS] receiver: %s %s\n", r.Method, r.URL.Path)
	})
	http.ListenAndServe(":9002", nil)
}
