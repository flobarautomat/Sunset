package main

import (
	"fmt"
	"os"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
	_ "nhooyr.io/websocket"
)

func main() {
	_ = chi.NewRouter()
	fmt.Println("moonrise server")
	os.Exit(0)
}
