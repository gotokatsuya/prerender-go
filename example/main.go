package main

import (
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/gotokatsuya/prerender-go/prerender"
)

func main() {
	prerenderHandler := prerender.New(prerender.NewOptions())

	http.Handle("/", prerenderHandler.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tpl := template.Must(template.ParseFiles("index.html"))
		tpl.Execute(w, nil)
	})))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	log.Printf("Listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
