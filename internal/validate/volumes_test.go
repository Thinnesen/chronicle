package validate

import "testing"

func TestVolumes(t *testing.T) {
	const vols = "volumeClaimTemplates: [{name: work, storage: 1Gi}]\nvolumes: [{name: scratch, emptyDir: {}}]\n"
	mount := func(m string) string {
		return vols + "entrypoint: main\ntemplates:\n- name: main\n  inputs: {artifacts: [{name: src, path: /src, raw: {data: x}}]}\n" +
			"  container: {image: x, command: [run], volumeMounts: [" + m + "]}\n"
	}
	runCases(t, []specCase{
		{name: "mounts of a claim and an emptyDir", spec: mount("{name: work, mountPath: /work}, {name: scratch, mountPath: /tmp/s, subPath: a/b}")},
		{name: "unknown volume", spec: mount("{name: cache, mountPath: /cache}"), want: []string{`volumeMounts[0].name: Not found: "cache"`}},
		{name: "two mounts at one path", spec: mount("{name: work, mountPath: /work}, {name: scratch, mountPath: /work/}"),
			want: []string{"volumeMounts[1].mountPath", "another mount uses this path"}, n: 1},
		{name: "a mount at an input artifact", spec: mount("{name: work, mountPath: /src}"), want: []string{"the input artifact src uses this path"}},
		{name: "a mount at the root", spec: mount("{name: work, mountPath: /}"), want: []string{"volumeMounts[0].mountPath"}},
		{name: "a mount in the chronicle directory", spec: mount("{name: work, mountPath: /var/run/chronicle-inputs/x}"),
			want: []string{"chronicle uses the paths below /var/run/chronicle"}},
		{name: "a subPath that leaves the volume", spec: mount("{name: work, mountPath: /w, subPath: ../etc}"), want: []string{"volumeMounts[0].subPath"}},
		{name: "an absolute subPath", spec: mount("{name: work, mountPath: /w, subPath: /etc}"), want: []string{"volumeMounts[0].subPath"}},
		{name: "a claim and a volume with one name",
			spec: "volumeClaimTemplates: [{name: work, storage: 1Gi}]\nvolumes: [{name: work, emptyDir: {}}]\n" +
				"entrypoint: main\ntemplates: [{name: main, container: {image: x}}]",
			want: []string{"spec.volumes[0].name: Duplicate value"}},
		{name: "a name of chronicle", spec: "volumes: [{name: chronicle-exec, emptyDir: {}}]\nentrypoint: main\ntemplates: [{name: main, container: {image: x}}]",
			want: []string{"spec.volumes[0].name", "chronicle uses names that start with chronicle-"}, n: 1},
	}, false)
}
