import { createContext, useCallback, useContext, useState, type ReactNode } from "react";

import "./Toast.css";

interface Message {
  id: number;
  text: string;
  kind: "done" | "error";
}

const Ctx = createContext<(text: string, kind?: Message["kind"]) => void>(() => {});

// ToastProvider shows short messages after an action. Screen readers read them through a live region.
export function ToastProvider({ children }: { children: ReactNode }) {
  const [messages, setMessages] = useState<Message[]>([]);
  const show = useCallback((text: string, kind: Message["kind"] = "done") => {
    const id = Date.now() + Math.random();
    setMessages((m) => [...m, { id, text, kind }]);
    setTimeout(() => setMessages((m) => m.filter((x) => x.id !== id)), kind === "error" ? 8000 : 4000);
  }, []);
  return (
    <Ctx.Provider value={show}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {messages.map((m) => (
          <p key={m.id} className={`toast toast-${m.kind}`}>
            {m.text}
          </p>
        ))}
      </div>
    </Ctx.Provider>
  );
}

export function useToast() {
  return useContext(Ctx);
}
