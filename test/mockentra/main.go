// Command mockentra is a mock of Microsoft Entra ID and Microsoft Graph for the e2e tests. Never deploy it.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Thinnesen/chronicle/internal/auth/oidctest"
)

// config is the file of the mock: the tenant, the issuer host and the users with their claims.
type config struct {
	Tenant string                    `json:"tenant"`
	Host   string                    `json:"host"`
	Users  map[string]map[string]any `json:"users"`
}

func main() {
	path := flag.String("config", "/etc/mockentra/config.json", "The configuration file.")
	flag.Parse()
	data, err := os.ReadFile(*path)
	if err != nil {
		log.Fatal(err)
	}
	var c config
	if err := json.Unmarshal(data, &c); err != nil {
		log.Fatal(err)
	}
	p := oidctest.NewHandler("/" + c.Tenant + "/v2.0")
	p.Issuer = c.Host + "/" + c.Tenant + "/v2.0"
	p.Users = c.Users
	p.Handle("POST /v1.0/me/getMemberObjects", p.GraphGroups)
	srv := &http.Server{Addr: ":8080", Handler: p.Mux(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
