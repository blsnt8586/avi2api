import type { ReactNode } from "react";

export function Metric({
  icon,
  label,
  value,
  detail,
  tone = "normal",
}: {
  icon: React.ReactNode;
  label: string;
  value: string | number;
  detail?: string;
  tone?: "normal" | "warning";
}) {
  return (
    <div className={`metric ${tone}`}>
      {icon}
      <span>{label}</span>
      <strong>{value}</strong>
      {detail && <small>{detail}</small>}
    </div>
  );
}
