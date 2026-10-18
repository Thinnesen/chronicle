// Command chronicle-server serves the chronicle API (gRPC and HTTP/JSON) and, later, the web UI.
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/auth"
	"github.com/Thinnesen/chronicle/internal/server"
)

func main() {
	var listen, authConfig string
	flag.StringVar(&listen, "listen", ":8080", "The address of the API. TLS ends at the ingress or the service mesh.")
	flag.StringVar(&authConfig, "auth-config", "", "The login configuration file. Without it, the API refuses all calls.")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("server")

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(chroniclev1alpha1.AddToScheme(scheme))

	mux := http.NewServeMux()
	var authn []auth.Authenticator
	if authConfig != "" {
		login, err := newLogin(authConfig)
		if err != nil {
			log.Error(err, "Failed to set up the login")
			os.Exit(1)
		}
		mux.Handle("/auth/", login.Handler())
		authn = []auth.Authenticator{login.SessionAuthenticator(), login.BearerAuthenticator()}
	} else {
		log.Info("No login configuration: the API refuses all calls")
	}
	api, err := server.NewHandler(server.Options{Config: ctrl.GetConfigOrDie(), Scheme: scheme, Authenticators: authn})
	if err != nil {
		log.Error(err, "Failed to create the API")
		os.Exit(1)
	}
	mux.Handle("/", api)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	// gRPC clients use HTTP/2 without TLS inside the cluster.
	protocols.SetUnencryptedHTTP2(true)
	srv := &http.Server{
		Addr:              listen,
		Handler:           mux,
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("Serving the API", "address", listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error(err, "The server stopped")
		os.Exit(1)
	}
}

func newLogin(path string) (*auth.OIDC, error) {
	cfg, err := auth.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return auth.NewOIDC(ctx, cfg, auth.NewMemoryStore(cfg.Session.TTL.Duration, cfg.Session.IdleTimeout.Duration))
}
