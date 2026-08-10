import * as React from "react";
import { cn } from "../../lib/utils";

export const Select = React.forwardRef<HTMLSelectElement, React.SelectHTMLAttributes<HTMLSelectElement>>(
  ({ className, ...props }, ref) => <select ref={ref} className={cn("min-h-10 rounded-md border border-[var(--border-strong)] bg-[var(--surface-raised)] px-3 py-2 text-sm text-[var(--text)] outline-none focus:border-[var(--cyan)]", className)} {...props} />,
);
Select.displayName = "Select";
