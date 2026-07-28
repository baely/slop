package main

import (
	"log"
	"net/http"
	"os"
	"time"
	_ "time/tzdata"

	"github.com/baileybutler/shoebox/internal/ingest"
	"github.com/baileybutler/shoebox/internal/smtpd"
	"github.com/baileybutler/shoebox/internal/store"
	"github.com/baileybutler/shoebox/internal/web"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	addr := env("ADDR", ":8080")
	smtpAddr := env("SMTP_ADDR", ":2525")
	dataDir := env("DATA_DIR", "./data")
	domain := env("SMTP_DOMAIN", "shoebox.baileys.dev")
	ingestAddr := env("INGEST_ADDR", "tax@baileys.dev")
	pw := os.Getenv("AUTH_PASSWORD")

	loc, err := time.LoadLocation(env("TZ", "Australia/Melbourne"))
	if err != nil {
		log.Printf("tz: %v, using UTC", err)
		loc = time.UTC
	}

	st, err := store.Open(dataDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	go func() {
		log.Printf("smtp: listening on %s (domain %s)", smtpAddr, domain)
		if err := smtpd.ListenAndServe(smtpAddr, domain, st, loc); err != nil {
			log.Fatalf("smtp: %v", err)
		}
	}()

	// Stored receipts may predate walk-back fixes or the generated email.pdf:
	// re-derive headers from raw.eml, then (re)build missing snapshots.
	go func() {
		for _, r := range st.List() {
			changed, err := ingest.ReparseHeaders(st, r, loc)
			if err != nil {
				log.Printf("reparse: %s: %v", r.ID, err)
			} else if changed {
				log.Printf("reparse: %s: headers updated", r.ID)
				st.RemoveGenerated(r.ID)
			}
			if r2, ok := st.Get(r.ID); ok {
				if err := ingest.BackfillPDF(st, r2); err != nil {
					log.Printf("backfill: %s: %v", r.ID, err)
				}
			}
		}
	}()

	if pw == "" {
		log.Printf("warning: AUTH_PASSWORD not set, web ui is open")
	}
	log.Printf("http: listening on %s (data %s)", addr, dataDir)
	log.Fatal(http.ListenAndServe(addr, web.New(st, pw, ingestAddr, loc)))
}
