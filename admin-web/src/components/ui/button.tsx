import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

const buttonVariants = cva(
  "inline-flex min-h-9 items-center justify-center gap-2 rounded-md border border-transparent px-3 py-2 text-sm font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--cyan)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg)] disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        primary: "bg-[var(--lime)] text-[var(--lime-ink)] hover:bg-[#ff8266]",
        secondary: "border-[var(--border)] bg-[var(--surface)] text-[var(--text-soft)] hover:border-[var(--border-strong)] hover:bg-[var(--surface-hover)] hover:text-[var(--text)]",
        ghost: "bg-transparent text-[var(--text-soft)] hover:bg-[var(--surface-hover)] hover:text-[var(--text)]",
        danger: "bg-[var(--red)] text-[#210708] hover:bg-[#ff9292]",
      },
      size: {
        sm: "min-h-8 px-2.5 py-1.5 text-xs",
        md: "min-h-9 px-3 py-2",
        lg: "min-h-11 px-4 py-2.5",
        icon: "size-9 min-h-9 px-0 py-0",
      },
    },
    defaultVariants: { variant: "primary", size: "md" },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild, ...props }, ref) => {
    const Component = asChild ? Slot : "button";
    return (
      <Component
        ref={ref}
        data-ui-button="true"
        data-variant={variant || "primary"}
        className={cn(buttonVariants({ variant, size }), className)}
        {...props}
      />
    );
  },
);
Button.displayName = "Button";

export { buttonVariants };
