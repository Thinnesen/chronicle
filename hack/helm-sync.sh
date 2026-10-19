#!/usr/bin/env bash
# helm-sync.sh copies the generated CRDs and the rules of the controller role into the Helm chart.
# It escapes "{{" for Helm and adds the keep policy to the CRDs.
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

# The rules of the controller role: keep the templated header of the chart, take the rules from controller-gen.
role=dist/chart/templates/rbac/manager-role.yaml
{
  sed '/^rules:$/q' "$role"
  sed '1,/^rules:$/d' config/rbac/role.yaml
} > "$role.new"
mv "$role.new" "$role"
