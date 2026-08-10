import type { HTMLAttributes } from "react";
import { cva, VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

const badgeVariants = cva(
  "inline-flex min-h-6 items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-semibold",
  {
    variants: {
      tone: {
        neutral: "border-[var(--border)] bg-[var(--surface-hover)] text-[var(--text-soft)]",
        success: "border-[#74d7b044] bg-[#74d7b014] text-[var(--green)]",
        warning: "border-[#ffb65c44] bg-[#ffb65c14] text-[var(--amber)]",
        info: "border-[#7dc8ff44] bg-[#7dc8ff14] text-[var(--cyan)]",
        danger: "border-[#f0787844] bg-[#f0787814] text-[var(--red)]",
      },
    },
    defaultVariants: { tone: "neutral" },
  },
);

export interface BadgeProps
  extends HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, tone, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ tone }), className)} {...props} />;
}
