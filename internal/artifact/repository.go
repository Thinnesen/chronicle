// Package artifact reads and writes artifacts and archived logs in the S3-compatible repository of a namespace.
package artifact

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Name is the name of the ConfigMap and of the Secret that configure the repository of a namespace.
const Name = "chronicle-artifact-repository"

// The keys of the Secret.
const (
	SecretAccessKey = "accessKey"
	SecretSecretKey = "secretKey"
)

// Repository is the artifact repository of a namespace. It comes from the ConfigMap.
type Repository struct {
	// Endpoint is the host and port of the S3 API, without a scheme.
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	Region   string `json:"region,omitempty"`
	// Insecure uses HTTP instead of HTTPS.
	Insecure bool `json:"insecure,omitempty"`
	// KeyPrefix is put before all keys.
	KeyPrefix string `json:"keyPrefix,omitempty"`
	// ArchiveLogs saves the log of each main container.
	ArchiveLogs bool `json:"archiveLogs,omitempty"`
	// CABundle is PEM certificates for an endpoint with a private CA. Without it, the system CAs apply.
	CABundle string `json:"caBundle,omitempty"`
}

// FromConfigMap reads a repository from the data of the ConfigMap.
func FromConfigMap(data map[string]string) (Repository, error) {
	r := Repository{
		Endpoint: data["endpoint"], Bucket: data["bucket"], Region: data["region"],
		KeyPrefix: strings.Trim(data["keyPrefix"], "/"), CABundle: data["caBundle"],
	}
	if r.CABundle != "" {
		if _, err := certPool(r.CABundle); err != nil {
			return r, err
		}
	}
	var err error
	for key, dst := range map[string]*bool{"insecure": &r.Insecure, "archiveLogs": &r.ArchiveLogs} {
		if v, ok := data[key]; ok {
			if *dst, err = strconv.ParseBool(v); err != nil {
				return r, fmt.Errorf("ConfigMap %s: %s is not true or false", Name, key)
			}
		}
	}
	host, port, err := net.SplitHostPort(r.Endpoint)
	if _, perr := strconv.Atoi(port); err != nil || perr != nil || host == "" || strings.Contains(r.Endpoint, "/") {
		return r, fmt.Errorf("ConfigMap %s: endpoint must be host:port without a scheme", Name)
	}
	if r.Bucket == "" {
		return r, fmt.Errorf("ConfigMap %s: bucket is required", Name)
	}
	return r, nil
}

// Prefix returns the key prefix of the files of one Pod.
func (r Repository) Prefix(namespace, workflowUID, podName string) string {
	return path.Join(r.KeyPrefix, namespace, workflowUID, podName)
}

// ArtifactKey returns the key of an output artifact.
func ArtifactKey(prefix, name string) string { return prefix + "/" + name + ".tgz" }

// LogKey returns the key of the archived log.
func LogKey(prefix string) string { return prefix + "/main.log" }

// ErrNotFound means that the repository has no object with the key.
var ErrNotFound = errors.New("the artifact is not in the repository")

// Store reads and writes the objects of one repository.
type Store struct {
	client *minio.Client
	bucket string
}

// Open connects to a repository with static credentials.
func Open(r Repository, accessKey, secretKey string) (*Store, error) {
	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("the Secret %s needs the keys %s and %s", Name, SecretAccessKey, SecretSecretKey)
	}
	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: !r.Insecure,
		Region: r.Region,
	}
	if r.CABundle != "" {
		pool, err := certPool(r.CABundle)
		if err != nil {
			return nil, err
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
		opts.Transport = tr
	}
	c, err := minio.New(r.Endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("connect to the artifact repository: %w", err)
	}
	return &Store{client: c, bucket: r.Bucket}, nil
}

// Put writes an object. A size of -1 means unknown.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("save %s: %w", key, err)
	}
	return nil
}

// Get opens an object and returns its size.
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", key, err)
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
			return nil, 0, ErrNotFound
		}
		return nil, 0, fmt.Errorf("read %s: %w", key, err)
	}
	return obj, info.Size, nil
}

// certPool reads the PEM certificates of a CA bundle.
func certPool(bundle string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(bundle)) {
		return nil, fmt.Errorf("ConfigMap %s: caBundle has no PEM certificate", Name)
	}
	return pool, nil
}
