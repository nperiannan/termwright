// termwright-team is a Termwright team server: the Termwrights of a team
// connect to it to see who uses which device. It keeps its data (team.json,
// its certificate and key) in one folder - /data in the Docker container.
//
//	termwright-team [-data DIR] [-listen :7443] [-name "Team name"]
//
// TW_DATA, TW_LISTEN and TW_TEAM_NAME do the same. At the first start it
// prints the owner setup code: the first Termwright to join with it is the
// team's owner (Termwright reads it when it sets the server up).
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/termwright/termwright/internal/team"
)

var Version = "dev"

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	data := flag.String("data", env("TW_DATA", "/data"), "folder for the team's data")
	listen := flag.String("listen", env("TW_LISTEN", ":7443"), "address to listen on")
	name := flag.String("name", env("TW_TEAM_NAME", "Team"), "team name (first start only)")
	version := flag.Bool("version", false, "print the version")
	flag.Parse()
	if *version {
		println(Version)
		return
	}
	logger := log.New(os.Stdout, "", log.LstdFlags)
	s, code, err := team.Open(*data, *name, Version, logger)
	if err != nil {
		logger.Fatalf("cannot start: %v", err)
	}
	logger.Printf("Termwright team server %s - listening on %s, data in %s", Version, *listen, *data)
	logger.Printf("certificate fingerprint=%s", s.Fingerprint())
	if code != "" {
		// read by Termwright's "Set Up a Team Server"; valid until the owner joins
		logger.Printf("TERMWRIGHT-TEAM owner-code=%s fingerprint=%s", code, s.Fingerprint())
	}
	srv := &http.Server{Addr: *listen, Handler: s.Handler(), TLSConfig: s.TLSConfig(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute, ErrorLog: log.New(os.Stderr, "http: ", 0)}
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("%v", err)
	}
}
