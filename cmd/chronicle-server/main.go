// Command chronicle-server serves the chronicle API (gRPC and HTTP/JSON) and the web UI.
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/otelconnect"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	chroniclev1alpha1 "github.com/Thinnesen/chronicle/api/v1alpha1"
	"github.com/Thinnesen/chronicle/internal/archive"
	"github.com/Thinnesen/chronicle/internal/auth"
	"github.com/Thinnesen/chronicle/internal/server"
	"github.com/Thinnesen/chronicle/internal/telemetry"
	"github.com/Thinnesen/chronicle/ui"
)

func main() {
	var listen, authConfig, tokenAudience, archiveDSNFile, basePath string
	flag.StringVar(&archiveDSNFile, "archive-dsn-file", "",
		"A file with the PostgreSQL DSN of the archive. Without it, deleted runs are gone.")
	flag.StringVar(&listen, "listen", ":8080", "The address of the API. TLS ends at the ingress or the service mesh.")
	flag.StringVar(&authConfig, "auth-config", "",
		"The login configuration file. Without it, only service account tokens work.")
	flag.StringVar(&tokenAudience, "token-audience", "chronicle",
		"The audience of the service account tokens that the API accepts. Empty turns these tokens off.")
	flag.StringVar(&basePath, "base-path", "/",
		"The path of chronicle on its host, for example /chronicle when a proxy serves it at https://example.com/chronicle.")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	basePath = server.NormalizeBasePath(basePath)
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("server")

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(chroniclev1alpha1.AddToScheme(scheme))

	restConfig := ctrl.GetConfigOrDie()
	shutdownTraces, err := telemetry.Setup(context.Background(), "chronicle-server")
	if err != nil {
		log.Error(err, "Failed to set up the traces")
		os.Exit(1)
	}
	defer func() { _ = shutdownTraces(context.Background()) }()
	mux := http.NewServeMux()
	var authn []auth.Authenticator
	if _, err := os.Stat(authConfig); authConfig != "" && errors.Is(err, os.ErrNotExist) {
		// The ConfigMap of the login is optional. Without it, only service account tokens work.
		log.Info("The login configuration file does not exist: the OIDC login is off", "file", authConfig)
		authConfig = ""
	}
	var store *archive.Store
	if archiveDSNFile != "" {
		if store, err = archive.OpenFile(context.Background(), archiveDSNFile); err != nil {
			log.Error(err, "Failed to open the archive")
			os.Exit(1)
		}
		defer store.Close()
	}
	if authConfig != "" {
		login, err := newLogin(authConfig, store)
		if err != nil {
			log.Error(err, "Failed to set up the login")
			os.Exit(1)
		}
		mux.Handle("/auth/", login.Handler())
		authn = []auth.Authenticator{login.SessionAuthenticator(), login.BearerAuthenticator()}
	} else {
		mux.Handle("/auth/", server.NoLoginHandler())
	}
	if tokenAudience != "" {
		own, err := client.New(restConfig, client.Options{Scheme: scheme})
		if err != nil {
			log.Error(err, "Failed to create the client")
			os.Exit(1)
		}
		authn = append(authn, &auth.TokenReviewAuthenticator{Client: own, Audiences: []string{tokenAudience}})
	}
	if len(authn) == 0 {
		log.Info("No login and no service account tokens: the API refuses all calls")
	}
	drain := make(chan struct{})
	apiOpts := server.Options{Config: restConfig, Scheme: scheme, Authenticators: authn, Drain: drain}
	if store != nil {
		apiOpts.Archive = store
		apiOpts.Tasks = store
	}
	if telemetry.Enabled() {
		tracing, err := otelconnect.NewInterceptor()
		if err != nil {
			log.Error(err, "Failed to set up the traces")
			os.Exit(1)
		}
		apiOpts.Interceptors = append(apiOpts.Interceptors, tracing)
	}
	api, err := server.NewHandler(apiOpts)
	if err != nil {
		log.Error(err, "Failed to create the API")
		os.Exit(1)
	}
	web := server.UIHandler(ui.Files(), basePath)
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/chronicle.v1.") || strings.HasPrefix(r.URL.Path, server.ArtifactPath) {
			api.ServeHTTP(w, r)
			return
		}
		web.ServeHTTP(w, r)
	}))
	// The probes call /healthz at the root, also with a base path.
	root := http.NewServeMux()
	root.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	root.Handle("/", server.WithBasePath(basePath, mux))

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	// gRPC clients use HTTP/2 without TLS inside the cluster.
	protocols.SetUnencryptedHTTP2(true)
	srv := &http.Server{
		Addr:              listen,
		Handler:           root,
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		close(drain)
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

// newLogin reads the login configuration and discovers the provider. It tries again for 5 minutes,
// because the provider can start after the server. With an archive, the sessions are in its database, so that
// all replicas share them and a restart keeps them.
func newLogin(path string, db *archive.Store) (*auth.OIDC, error) {
	cfg, err := auth.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	var store auth.Store = auth.NewMemoryStore(cfg.Session.TTL.Duration, cfg.Session.IdleTimeout.Duration)
	if db != nil {
		store = db.Sessions(cfg.Session.TTL.Duration, cfg.Session.IdleTimeout.Duration)
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		login, err := auth.NewOIDC(ctx, cfg, store)
		cancel()
		if err == nil || time.Now().After(deadline) {
			return login, err
		}
		ctrl.Log.WithName("server").Info("The OIDC provider is not ready, trying again", "reason", err.Error())
		time.Sleep(5 * time.Second)
	}
}
