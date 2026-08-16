import type { MediaKind, Provider } from "../shared/types";
import {
  isRegisteredProviderID,
  providerRegistry,
  type RegisteredProviderID,
} from "../shared/providers";
import { ProviderSwitcher } from "./provider-switcher";

function providerOptions(configured: Provider[]) {
  return (Object.keys(providerRegistry) as RegisteredProviderID[]).map((id) => {
    const current = configured.find((provider) => provider.id === id);
    if (current) return current;
    const fallback = providerRegistry[id];
    return {
      id,
      display_name: fallback.displayName,
      enabled: true,
      adapter_registered: true,
      auth_type: "",
      credit_unit: id === "adobe" ? "BKS" : "credits",
      capabilities: [...fallback.capabilities],
      catalog_sync: false,
      models: {
        image: [...fallback.models.image],
        video: [...fallback.models.video],
        audio: [...fallback.models.audio],
      },
    } satisfies Provider;
  });
}

export function GenerationProviderSelector({
  providers,
  value,
  capability,
  onChange,
}: {
  providers: Provider[];
  value: RegisteredProviderID;
  capability: MediaKind;
  onChange: (providerID: RegisteredProviderID) => void;
}) {
  return (
    <div className="playground-provider-field">
      <span>平台</span>
      <ProviderSwitcher
        providers={providerOptions(providers)}
        value={value}
        capability={capability}
        compact
        onChange={(providerID) => {
          if (isRegisteredProviderID(providerID)) onChange(providerID);
        }}
      />
    </div>
  );
}
