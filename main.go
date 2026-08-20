// Command task102-schemaregistry runs the schema-registry HTTP service. With
// the --smoke-test flag it runs an in-process self-check that exercises the
// registry, persistence and restart recovery, then exits.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"task102-schemaregistry/internal/httpapi"
	"task102-schemaregistry/internal/registry"
	"task102-schemaregistry/internal/store"
)

func main() {
	var (
		addr      = flag.String("addr", ":8080", "HTTP listen address")
		dbPath    = flag.String("db", "schemareg.db", "SQLite database path")
		smokeTest = flag.Bool("smoke-test", false, "run an in-process self-check and exit")
	)
	flag.Parse()

	if *smokeTest {
		if err := runSmoke(); err != nil {
			fmt.Fprintf(os.Stderr, "smoke-test FAILED: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("smoke-test OK")
		return
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()
	reg := registry.New(s)
	mux := httpapi.NewMux(reg)
	log.Printf("schema-registry listening on %s (db=%s)", *addr, *dbPath)
	srv := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down")
}
