import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "../../lib/utils";

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export function DialogContent({
  className,
  children,
  showClose = true,
  ...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & { showClose?: boolean }) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-80 bg-black/70 backdrop-blur-[2px] data-[state=open]:animate-in data-[state=closed]:animate-out" />
      <DialogPrimitive.Content
        className={cn(
          "fixed left-1/2 top-1/2 z-90 grid max-h-[90dvh] w-[min(620px,calc(100vw-32px))] -translate-x-1/2 -translate-y-1/2 gap-4 overflow-y-auto rounded-lg border border-[var(--border-strong)] bg-[var(--surface-raised)] p-5 text-[var(--text)] shadow-2xl focus:outline-none",
          className,
        )}
        {...props}
      >
        {children}
        {showClose && (
          <DialogPrimitive.Close className="absolute right-3 top-3 grid size-9 place-items-center rounded-md text-[var(--text-muted)] hover:bg-[var(--surface-hover)] hover:text-[var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--cyan)]" aria-label="关闭">
            <X size={18} />
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

export const DialogTitle = DialogPrimitive.Title;
export const DialogDescription = DialogPrimitive.Description;
