import type { MediaKind, Provider } from "../shared/types";
import { providerCapabilityText, providerDefinition, providerSupports } from "../shared/providers";

export function ProviderBadge({ providerID, providers = [] }: { providerID: string; providers?: Provider[] }) {
  const provider = providerDefinition(providerID, providers.find((item) => item.id === providerID));
  return <span className={`provider-badge provider-${provider.id}`}><i />{provider.shortName}</span>;
}

export function ProviderSwitcher({
  providers,
  value,
  onChange,
  capability,
  includeAll = false,
  includeDisabled = false,
  compact = false,
}: {
  providers: Provider[];
  value: string;
  onChange: (providerID: string) => void;
  capability?: MediaKind;
  includeAll?: boolean;
  includeDisabled?: boolean;
  compact?: boolean;
}) {
  const visible = providers.filter((provider) => provider.adapter_registered !== false && (includeDisabled || provider.enabled) && (!capability || providerSupports(provider, capability)));
  return (
    <div className={`provider-switcher${compact ? " compact" : ""}`} role="group" aria-label="选择业务平台">
      {includeAll && (
        <button type="button" className={value === "all" ? "active provider-all" : "provider-all"} onClick={() => onChange("all")} aria-pressed={value === "all"}>
          <span className="provider-mark" />
          <span><strong>全部平台</strong>{!compact && <small>跨平台汇总</small>}</span>
        </button>
      )}
      {visible.map((provider) => {
        const presentation = providerDefinition(provider.id, provider);
        return (
          <button type="button" key={provider.id} className={`${value === provider.id ? "active " : ""}${provider.enabled ? "" : "disabled-provider "}provider-${provider.id}`} onClick={() => onChange(provider.id)} aria-pressed={value === provider.id}>
            <span className="provider-mark" />
            <span><strong>{presentation.displayName}</strong>{!compact && <small>{provider.enabled ? providerCapabilityText(provider) : "已停用 · 历史数据"}</small>}</span>
          </button>
        );
      })}
    </div>
  );
}
