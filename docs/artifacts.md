# Artifacts and the log archive

An artifact is a file or a directory that a step saves and a later step uses. chronicle keeps artifacts in an S3-compatible store, for example Amazon S3. The end-to-end tests use SeaweedFS. The same store can also keep the logs of all steps. Then you can read them after the Pods are deleted.

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
  # caBundle: |               # optional: PEM certificates of a private CA of the endpoint
  #   -----BEGIN CERTIFICATE-----
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
- A `dag` or `steps` template can pass on an artifact of one of its tasks or steps. For this, it has an output artifact with only `from`, for example `{name: report, from: "{{tasks.render.outputs.artifacts.report}}"}`. Its caller reads it as `{{tasks.<task>.outputs.artifacts.report}}`.
- `from` must be exactly one placeholder: `{{tasks.<task>.outputs.artifacts.<name>}}`, `{{steps.<step>.outputs.artifacts.<name>}}` or `{{inputs.artifacts.<name>}}`.
- An input artifact can have any path, for example `/src` or `/data/model.bin`. The init container downloads it, and chronicle mounts it at its path. An output artifact needs a path that the step user (65532) can write, for example below `/tmp`.
- Set `optional: true` on an output that a step does not always make. Set it also on an input that a caller does not always give.
- A fan-out (`withItems`, `withParam`) has no artifact outputs.
- An input artifact can unpack to at most 8 GiB.

## 3. Give a file as text or from a URL

An input artifact, or an artifact argument, can bring its own content. These need no artifact repository.

```yaml
spec:
  entrypoint: main
  arguments:
    artifacts:
    - name: config              # the workflow gives this file to the entrypoint
      raw:
        data: |
          region: eu
  templates:
  - name: main
    inputs:
      parameters: [{name: version, value: "1.31.0"}]
      artifacts:
      - name: config
        path: /tmp/config.yaml
      - name: kubectl
        path: /tmp/bin/kubectl
        mode: 0755
        http:
          url: "https://dl.k8s.io/release/v{{inputs.parameters.version}}/bin/linux/amd64/kubectl"
    container:
      image: alpine:3.23
      command: [sh, -c, "cat /tmp/config.yaml && /tmp/bin/kubectl version --client"]
```

- `raw.data` is the text of the file, at most 64 KiB. Placeholders in it stay as they are.
- `http.url` is a file that the step downloads with a GET request from its own Pod. The download happens before the command starts. The URL can have placeholders. The step saves the file as it is: it does not unpack an archive.
- `mode` sets the file mode, for example `0755` for a program. The default is `0644`.
- `spec.arguments.artifacts` gives artifacts to the entrypoint and to the exit handler. They need `raw` or `http`.
- A `dag` or `steps` template passes such an input on with `from: "{{inputs.artifacts.<name>}}"`.
- An error message names the URL without its query, because a signed URL has its token there. But the URL is in the Workflow spec. The resolved URL is in the node status, and the step Pod has it in its environment. Each reader of the Workflow or of the Pods in the namespace can see it. Do not put a token into a URL that these readers must not see. Download such a file in your command, with the token from a Secret.

## 4. Download an artifact

Open the run in the UI, select the step and open **Summary**. Each output artifact has a download link. The file is a tar.gz archive. You need `get` permission on Workflows in the namespace.

## 5. Read archived logs

With `archiveLogs: "true"`, the UI and `chronicle logs <run>` show the log of a step after its Pod is deleted. For example, `podGC` or the TTL deletes the Pod. The UI marks such a log. You need `get` permission on `pods/log` in the namespace. The archive keeps the first 32 MiB of each log.

## Problems

| Problem | Cause and fix |
|---|---|
| "the namespace has no artifact repository" | Create the ConfigMap and the Secret (step 1). |
| "the Secret chronicle-artifact-repository needs the keys accessKey and secretKey" | Add both keys to the Secret. |
| "input artifact ...: permission denied" | The container user cannot write the path. Use a path below `/tmp`. |
| "The specified bucket does not exist" | Create the bucket in the store. chronicle does not create buckets. |
