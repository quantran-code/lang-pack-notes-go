package main

import (
	"log"
	"os"

	"github.com/quantran-code/lang-pack-notes-go/internal/database"
	"github.com/quantran-code/lang-pack-notes-go/internal/httpapi"
)

func main() {
	dsn := os.Getenv("DB_PATH")
	if dsn == "" {
		dsn = database.MemoryDSN
	}
	db, err := database.Open(dsn)
	if err != nil {
		log.Fatal(err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(httpapi.NewRouter(db).Start(":" + port))
}
