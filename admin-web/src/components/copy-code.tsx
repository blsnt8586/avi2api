import { useState } from "react";
import { Check, Copy } from "lucide-react";

export function CopyCode({
  value,
  title = "复制请求示例",
}: {
  value: string;
  title?: string;
}) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(value);
      } else {
        const area = document.createElement("textarea");
        area.value = value;
        area.style.position = "fixed";
        area.style.opacity = "0";
        document.body.appendChild(area);
        area.select();
        document.execCommand("copy");
        area.remove();
      }
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  }
  return (
    <button
      className="secondary copy-button"
      onClick={copy}
      title={title}
    >
      {copied ? <Check /> : <Copy />}
      {copied ? "已复制" : "复制"}
    </button>
  );
}
