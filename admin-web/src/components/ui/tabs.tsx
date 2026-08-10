import * as TabsPrimitive from "@radix-ui/react-tabs";
import { cn } from "../../lib/utils";

export const Tabs = TabsPrimitive.Root;
export const TabsContent = TabsPrimitive.Content;

export function TabsList({ className, ...props }: React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>) {
  return <TabsPrimitive.List className={cn("inline-flex min-h-10 items-center gap-1 rounded-md border border-[var(--border)] bg-[var(--surface)] p-1", className)} {...props} />;
}

export function TabsTrigger({ className, ...props }: React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>) {
  return <TabsPrimitive.Trigger className={cn("min-h-8 rounded px-3 text-xs font-semibold text-[var(--text-muted)] hover:text-[var(--text)] data-[state=active]:bg-[var(--surface-hover)] data-[state=active]:text-[var(--text)]", className)} {...props} />;
}
