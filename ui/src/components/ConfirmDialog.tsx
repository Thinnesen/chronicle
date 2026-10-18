import * as Dialog from "@radix-ui/react-dialog";
import type { ReactNode } from "react";

import "./ConfirmDialog.css";

// ConfirmDialog asks before an action that cannot be undone. The confirm button names the action.
export function ConfirmDialog(props: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  children: ReactNode;
  confirm: string;
  danger?: boolean;
  onConfirm: () => void;
}) {
  return (
    <Dialog.Root open={props.open} onOpenChange={props.onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog">
          <Dialog.Title className="dialog-title">{props.title}</Dialog.Title>
          <Dialog.Description asChild>
            <div className="dialog-body">{props.children}</div>
          </Dialog.Description>
          <div className="dialog-buttons">
            <Dialog.Close className="button">Cancel</Dialog.Close>
            <button
              type="button"
              className={props.danger ? "button button-danger" : "button button-primary"}
              onClick={() => {
                props.onConfirm();
                props.onOpenChange(false);
              }}
            >
              {props.confirm}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
