package main

import (
	"log"
	"net/http"

	"k8s-proxmox-manager/backend/internal/server"
)

func main() {
	app, err := server.NewApp()
	if err != nil {
		log.Fatalf("failed to initialize app: %v", err)
	}
	defer app.DB.Close()

	log.Printf("api listening on %s", app.Config.Address)
	if err := http.ListenAndServe(app.Config.Address, app.Router); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
