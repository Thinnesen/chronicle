package convert

import "fmt"

// volumeClaimTemplates converts Argo claim templates, which are full PersistentVolumeClaims, to the small chronicle
// form. Fields such as dataSource could copy the data of another claim, so they are dropped.
func (c *converter) volumeClaimTemplates(in []any, path string) []any {
	var out []any
	for i, v := range in {
		vp := index(path, i)
		meta, spec := asObj(asObj(v)["metadata"]), asObj(asObj(v)["spec"])
		c.pick(asObj(v), vp, nil, "metadata", "spec")
		c.pick(meta, join(vp, "metadata"), nil, fName)
		res := asObj(spec["resources"])
		c.pick(spec, join(vp, "spec"), nil, "accessModes", "storageClassName", "resources")
		c.pick(res, join(vp, "spec.resources"), nil, "requests")
		storage := asObj(res["requests"])["storage"]
		if meta[fName] == nil || storage == nil {
			c.warn(vp, "a claim template needs metadata.name and spec.resources.requests.storage: it is dropped")
			continue
		}
		claim := obj{fName: meta[fName], "storage": storage}
		for _, k := range []string{"accessModes", "storageClassName"} {
			if spec[k] != nil {
				claim[k] = spec[k]
			}
		}
		c.volumes[fmt.Sprint(meta[fName])] = true
		out = append(out, claim)
	}
	return out
}

// podVolumes keeps the emptyDir, secret and configMap volumes. Other volumes, such as an existing claim, are dropped.
func (c *converter) podVolumes(in []any, path string) []any {
	var out []any
	for i, v := range in {
		vol := asObj(v)
		name := fmt.Sprint(vol[fName])
		kept := obj{fName: name}
		for _, k := range []string{"emptyDir", "secret", "configMap"} {
			if vol[k] != nil {
				kept[k] = vol[k]
			}
		}
		if len(kept) != 2 {
			c.warn(index(path, i), "chronicle supports emptyDir, secret and configMap volumes: the volume %s is dropped", name)
			continue
		}
		c.volumes[name] = true
		out = append(out, kept)
	}
	return out
}

// volumeMounts keeps the mounts of the volumes that the converter kept.
func (c *converter) volumeMounts(in []any, path string) []any {
	var out []any
	for i, m := range in {
		mount := asObj(m)
		mp := index(path, i)
		if !c.volumes[fmt.Sprint(mount[fName])] {
			c.warn(mp, "the volume %v is dropped, so the mount is dropped", mount[fName])
			continue
		}
		out = append(out, c.pick(mount, mp, []string{fName, "mountPath", "subPath", "readOnly"}))
	}
	return out
}
