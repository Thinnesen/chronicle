import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { useState } from "react";

import { workflows } from "../api/client";
import type { TemplateSummary } from "../gen/chronicle/v1/template_pb";
import { actionError } from "../lib/errors";
import { useToast } from "./Toast";

// RunForm starts a run of a template. Each workflow parameter is a field. A list of permitted values is a select. A
// parameter that reads a ConfigMap is optional. The form does not send an empty field, so the run reads the
// ConfigMap.
export function RunForm({ namespace, template }: { namespace: string; template: TemplateSummary }) {
  const fromConfigMap = (name: string) => !!template.parameters.find((p) => p.name === name)?.configMapName;
  const required = (p: TemplateSummary["parameters"][number]) => !p.hasDefault && !p.configMapName;
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(template.parameters.map((p) => [p.name, p.hasDefault ? p.default : ""])),
  );
  // startAt is a local time from the field, or empty to start now.
  const [startAt, setStartAt] = useState("");
  const navigate = useNavigate();
  const toast = useToast();
  const submit = useMutation({
    mutationFn: () =>
      workflows.submitWorkflow({
        namespace,
        source: { case: "template", value: { name: template.name, clusterScope: template.clusterScope } },
        parameters: Object.fromEntries(Object.entries(values).filter(([name, v]) => v !== "" || !fromConfigMap(name))),
        startAt: startAt ? timestampFromDate(new Date(startAt)) : undefined,
      }),
    onSuccess: (resp) => {
      const name = resp.summary?.name ?? "";
      toast(startAt ? `Scheduled ${name} for ${new Date(startAt).toLocaleString()}` : `Started ${name}`);
      void navigate({ to: "/ns/$namespace/runs/$name", params: { namespace, name } });
    },
    onError: (err) => toast(actionError("run templates", namespace, err), "error"),
  });
  return (
    <form
      className="panel run-form"
      onSubmit={(e) => {
        e.preventDefault();
        submit.mutate();
      }}
    >
      <h2>Run this template</h2>
      {template.parameters.length === 0 && <p className="quiet">This template has no parameters.</p>}
      {template.parameters.map((p) => (
        <label key={p.name} className="field">
          <span>
            {p.name}
            {required(p) && <span className="quiet"> (required)</span>}
          </span>
          {p.enum.length > 0 ? (
            <select value={values[p.name]} onChange={(e) => setValues({ ...values, [p.name]: e.target.value })} required={required(p)}>
              {!p.hasDefault && <option value="">{p.configMapName ? "From the ConfigMap" : "Choose a value"}</option>}
              {p.enum.map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
          ) : (
            <input
              value={values[p.name]}
              onChange={(e) => setValues({ ...values, [p.name]: e.target.value })}
              required={required(p)}
              placeholder={p.configMapName ? `${p.configMapName}: ${p.configMapKey}` : undefined}
            />
          )}
          {p.configMapName && (
            <small className="quiet">
              Empty uses key <code>{p.configMapKey}</code> of ConfigMap <code>{p.configMapName}</code>.
            </small>
          )}
          {p.description && <small className="quiet">{p.description}</small>}
        </label>
      ))}
      <label className="field">
        <span>
          Start at <span className="quiet">(optional)</span>
        </span>
        <input type="datetime-local" value={startAt} onChange={(e) => setStartAt(e.target.value)} />
        <small className="quiet">Empty starts the run now. A later time keeps the run Pending until then.</small>
      </label>
      <button type="submit" className="button button-primary" disabled={submit.isPending}>
        {submit.isPending ? "Starting…" : startAt ? "Schedule the run" : "Start the run"}
      </button>
    </form>
  );
}
