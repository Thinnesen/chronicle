#!/usr/bin/env bash
# test-chart.sh installs the Helm chart into its own Kind cluster and runs a workflow. Run it from the
# repository root (make test-chart). It deletes the cluster at the end.
set -euo pipefail

CLUSTER=chronicle-chart
export KUBECONFIG=bin/$CLUSTER.kubeconfig
IMG=chronicle:chart-test
CERT_MANAGER_VERSION=v1.21.2
kubectl() { bin/kubectl "$@"; }
helm() { HELM_CACHE_HOME=bin/helm-home/cache HELM_CONFIG_HOME=bin/helm-home/config HELM_DATA_HOME=bin/helm-home/data bin/helm "$@"; }
cleanup() {
  status=$?
  if [ $status -ne 0 ]; then
    kubectl get pods -A || true
    kubectl -n chronicle-system logs deploy/chronicle-controller-manager --tail=50 || true
  fi
  bin/kind delete cluster --name "$CLUSTER" --kubeconfig "$KUBECONFIG" >/dev/null 2>&1 || true
  exit $status
}
trap cleanup EXIT

echo "== Cluster and image"
bin/kind create cluster --name "$CLUSTER" --kubeconfig "$KUBECONFIG" --wait 2m
make docker-build IMG="$IMG" >/dev/null
bin/kind load docker-image "$IMG" --name "$CLUSTER"

echo "== cert-manager $CERT_MANAGER_VERSION"
helm repo add jetstack https://charts.jetstack.io --force-update >/dev/null
helm install cert-manager jetstack/cert-manager --version "$CERT_MANAGER_VERSION" -n cert-manager --create-namespace \
  --set crds.enabled=true --wait --timeout 5m >/dev/null

echo "== chronicle chart"
helm install chronicle dist/chart -n chronicle-system --create-namespace \
  --set manager.image.repository=chronicle --set manager.image.tag=chart-test --set manager.image.pullPolicy=Never \
  --wait --timeout 5m
kubectl -n chronicle-system rollout status deploy/chronicle-server --timeout=2m

echo "== A workflow"
kubectl create namespace chart-test
kubectl apply -n chart-test -f test/e2e/testdata/dag-succeeds.yaml
for _ in $(seq 1 60); do
  phase=$(kubectl get workflow dag-succeeds -n chart-test -o jsonpath='{.status.phase}')
  [ "$phase" = Succeeded ] && break
  [ "$phase" = Failed ] || [ "$phase" = Error ] && { echo "the workflow is $phase"; exit 1; }
  sleep 3
done
[ "$phase" = Succeeded ] || { echo "the workflow did not finish: $phase"; exit 1; }

echo "== The server and the user roles"
kubectl -n chronicle-system port-forward svc/chronicle-server 18080:80 >/dev/null 2>&1 &
forward=$!
sleep 3
code=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18080/healthz)
kill $forward
[ "$code" = 200 ] || { echo "the server answered $code"; exit 1; }
for role in chronicle-viewer chronicle-editor chronicle-admin chronicle-task-worker; do
  kubectl get clusterrole "$role" >/dev/null
done
# can-i exits with 1 when the answer is no, which is the expected answer here.
answer=$(kubectl auth can-i create workflows -n chart-test --as=system:serviceaccount:chart-test:default || true)
[ "$answer" = no ] || { echo "a service account without a binding can create workflows"; exit 1; }

echo "The chart works."
