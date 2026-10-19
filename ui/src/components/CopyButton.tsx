import { Check, Copy } from "lucide-react";
import { useState } from "react";

// copyText copies text. The clipboard API needs HTTPS, so on plain HTTP a hidden text field does the copy.
async function copyText(text: string): Promise<void> {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const field = document.createElement("textarea");
  field.value = text;
  field.setAttribute("readonly", "");
  field.style.position = "fixed";
  field.style.opacity = "0";
  document.body.append(field);
  field.select();
  document.execCommand("copy");
  field.remove();
}

// CopyButton copies a value, for example a Pod name, and shows a check mark for a moment.
export function CopyButton({ text, label }: { text: string; label: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className="icon-button copy-button"
      aria-label={done ? "Copied" : label}
      title={label}
      onClick={() => {
        void copyText(text).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        });
      }}
    >
      {done ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
    </button>
  );
}
