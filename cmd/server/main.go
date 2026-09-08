package main

import (
	"flag"
	"log"
	"net/http"
)

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func main() {
	listenAddr := flag.String("listen", ":8080", "address to listen on")
	flag.Parse()

	log.Printf("main: listening on %s", *listenAddr)
	if err := http.ListenAndServe(*listenAddr, newMux()); err != nil {
		log.Fatalf("main: serve: %v", err)
	}
}
