import { useState } from "react";

export function CodeBlock({
  body,
  lang,
}: {
  body: string;
  lang?: "bash" | "yaml" | "powershell";
}) {
  const [copied, setCopied] = useState(false);
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(body);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      // Clipboard API can be blocked on insecure contexts (file://, http
      // without TLS). Silently swallow — the user can still triple-click
      // the text to select it.
    }
  };
  return (
    <div className="relative group rounded-lg border border-slate-200 bg-slate-900 overflow-hidden">
      <div className="flex items-center justify-between px-4 py-2 border-b border-slate-800 bg-slate-800/50">
        <span className="text-xs font-mono text-slate-400">{lang ?? "shell"}</span>
        <button
          type="button"
          onClick={onCopy}
          className="text-xs font-medium text-slate-300 hover:text-white px-2 py-1 rounded transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {copied ? "Copied!" : "Copy"}
        </button>
      </div>
      <pre className="p-4 overflow-x-auto text-sm leading-relaxed text-slate-100 font-mono">
        <code>{body}</code>
      </pre>
    </div>
  );
}
