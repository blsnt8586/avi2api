import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "../../lib/utils";

export const Sheet = DialogPrimitive.Root;
export const SheetTrigger = DialogPrimitive.Trigger;
export const SheetClose = DialogPrimitive.Close;

export function SheetContent({ className, children, ...props }: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-80 bg-black/65 backdrop-blur-[2px]" />
      <DialogPrimitive.Content
        className={cn("fixed inset-y-0 right-0 z-90 w-[min(620px,100vw)] overflow-y-auto border-l border-[var(--border-strong)] bg-[var(--surface-raised)] p-5 text-[var(--text)] shadow-2xl focus:outline-none", className)}
        {...props}
      >
        {children}
        <DialogPrimitive.Close className="absolute right-3 top-3 grid size-9 place-items-center rounded-md text-[var(--text-muted)] hover:bg-[var(--surface-hover)] hover:text-[var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--cyan)]" aria-label="关闭">
          <X size={18} />
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

export const SheetTitle = DialogPrimitive.Title;
export const SheetDescription = DialogPrimitive.Description;
