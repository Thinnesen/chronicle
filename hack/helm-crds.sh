#!/usr/bin/env bash
# helm-crds.sh copies the generated CRDs into the Helm chart. It escapes "{{" for Helm and adds the keep policy.
set -euo pipefail
for f in config/crd/bases/*.yaml; do
  base=${f##*_}
  out=dist/chart/templates/crd/${base%.yaml}.chronicle.thinnesen.com.yaml
  {
    echo '{{- if .Values.crd.enabled }}'
    sed -e '/^---$/d' -e 's/{{/{{ "{{" }}/g' "$f" | awk '
      { print }
      /^  annotations:$/ && !done {
        print "    {{- if .Values.crd.keep }}"
        print "    \"helm.sh/resource-policy\": keep"
        print "    {{- end }}"
        done = 1
      }'
    echo '{{- end }}'
  } > "$out"
done
