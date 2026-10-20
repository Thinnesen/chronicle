import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { crons } from "../api/client";
import { actionError } from "../lib/errors";
import { ConfirmDialog } from "./ConfirmDialog";
import { useToast } from "./Toast";

// BackfillDialog starts the runs of the schedule times in a range of days, for example days that the schedule missed.
// A time that has its run already is skipped, as with chronicle cron backfill.
export function BackfillDialog({
  namespace,
  name,
  open,
  onOpenChange,
}: {
  namespace: string;
  name: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const toast = useToast();
  const client = useQueryClient();
  const backfill = useMutation({
    mutationFn: () =>
      crons.backfillCronWorkflow({
        namespace,
        name,
        from: timestampFromDate(new Date(`${from}T00:00:00`)),
        to: timestampFromDate(new Date(`${to}T23:59:59`)),
      }),
    onSuccess: (resp) => {
      const runs =
        resp.created.length === 1 ? "1 run" : `${resp.created.length} runs`;
      const had =
        resp.existing.length === 0
          ? ""
          : `; ${resp.existing.length === 1 ? "1 time had" : `${resp.existing.length} times had`} its run already`;
      toast(`Started ${runs}${had}`);
      void client.invalidateQueries({
        queryKey: ["runs", namespace, "cron", name],
      });
    },
    onError: (err) =>
      toast(actionError("run templates", namespace, err), "error"),
  });
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Backfill ${name}`}
      confirm="Start the runs"
      onConfirm={() => {
        if (from && to && from <= to) backfill.mutate();
        else
          toast(
            "Give a start day and an end day that is not before it.",
            "error",
          );
      }}
    >
      <p>
        Start a run for each schedule time in these days, also when the schedule
        is suspended. A time that has its run already is skipped. One backfill
        starts at most 100 runs.
      </p>
      <div className="field-stack">
        <label className="field">
          <span>From</span>
          <input
            type="date"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
        </label>
        <label className="field">
          <span>To</span>
          <input
            type="date"
            value={to}
            onChange={(e) => setTo(e.target.value)}
          />
        </label>
      </div>
    </ConfirmDialog>
  );
}
