# Artifacts and the log archive

An artifact is a file or a directory that a step saves and a later step uses. chronicle keeps artifacts in an S3-compatible store, for example Amazon S3, Google Cloud Storage (S3 API), Ceph RGW or SeaweedFS. The same store can also keep the logs of all steps, so that you can read them after the Pods are deleted.

## 1. Configure the repository of a namespace

Each namespace has its own repository. Create a ConfigMap and a Secret, both with the name `chronicle-artifact-repository`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: chronicle-artifact-repository
  namespace: data-prod
data:
  endpoint: s3.eu-central-1.amazonaws.com:443   # host:port, no scheme
  bucket: data-prod-runs
  region: eu-central-1        # optional
  keyPrefix: chronicle        # optional
  archiveLogs: "true"         # optional: keep the log of each step
  # insecure: "true"          # only for a store without TLS in a test cluster
---
apiVersion: v1
kind: Secret
metadata:
  name: chronicle-artifact-repository
  namespace: data-prod
stringData:
  accessKey: <access key>
  secretKey: <secret key>
```

Give the credentials access only to this bucket (or this prefix). All workflow steps in the namespace can use them.

## 2. Pass an artifact between steps

```yaml
templates:
- name: main
  dag:
    tasks:
    - name: make
      template: make
    - name: use
      template: use
      depends: make
      arguments:
        artifacts:
        - name: data
          from: "{{tasks.make.outputs.artifacts.data}}"
- name: make
  outputs:
    artifacts:
    - name: data
      path: /tmp/out          # a file or a directory
  script:
    image: busybox
    source: mkdir -p /tmp/out && date > /tmp/out/now.txt
- name: use
  inputs:
    artifacts:
    - name: data
      path: /tmp/in
  script:
    image: busybox
    source: cat /tmp/in/now.txt
```

Rules:

- Only a `container` template with a `command`, or a `script` template, can save or download artifacts.
- `from` must be exactly one placeholder: `{{tasks.<task>.outputs.artifacts.<name>}}`, `{{steps.<step>.outputs.artifacts.<name>}}` or `{{inputs.artifacts.<name>}}`.
- Use a path that the container user (65532) can write, for example below `/tmp`.
- Set `optional: true` on an output that a step does not always make, or on an input that a caller does not always give.
- A fan-out (`withItems`, `withParam`) has no artifact outputs.
- An input artifact can unpack to at most 8 GiB.

## 3. Download an artifact

Open the run in the UI, select the node and open **Details**. Each output artifact has a download link. The file is a tar.gz archive. You need `get` permission on Workflows in the namespace.

## 4. Read archived logs

With `archiveLogs: "true"`, the UI and `chronicle logs <run>` show the log of a step after its Pod is deleted, for example by `podGC` or by the TTL. The UI marks such a log. You need `get` permission on `pods/log` in the namespace. The archive keeps the first 32 MiB of each log.

## Problems

| Problem | Cause and fix |
|---|---|
| "the namespace has no artifact repository" | Create the ConfigMap and the Secret (step 1). |
| "the Secret chronicle-artifact-repository needs the keys accessKey and secretKey" | Add both keys to the Secret. |
| "input artifact ...: permission denied" | The container user cannot write the path. Use a path below `/tmp`. |
| "The specified bucket does not exist" | Create the bucket in the store. chronicle does not create buckets. |
