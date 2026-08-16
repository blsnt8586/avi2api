# Account Pool Design QA

- Source visual truth paths:
  - `C:/Users/zhang/AppData/Local/Temp/codex-clipboard-5172f0c6-6997-4292-a46e-f3adea963db8.png`
  - `C:/Users/zhang/AppData/Local/Temp/codex-clipboard-76229f4c-597f-408c-a98b-bdc955b49447.png`
  - `C:/Users/zhang/AppData/Local/Temp/codex-clipboard-62d19a7e-c7c4-44fd-969b-843ef50be9b8.png`
- Implementation: `http://45.59.128.219:18080/`, account pool view
- Viewports: 1280 x 720 desktop, 390 x 844 mobile
- State: production data; default list, one selected account, archive confirmation, add-account sheet, both Leonardo auth modes
- Browser evidence: in-app browser DOM, computed layout metrics, control state, and production asset checks

## Full-view comparison evidence

- Header and every account row resolve to the same eleven computed grid tracks.
- At 1280 px, the page has no horizontal overflow. The account table is a block scroll container with a 924 px viewport and 1320 px internal grid.
- At 390 px, the desktop table is hidden, account cards are shown, and the document has no horizontal overflow.
- The add-account sheet remains contained in the viewport and scrolls vertically; platform, auth method, credential, and routing sections retain their order.

## Focused region evidence

- Selection column: header and row checkboxes occupy the same 42 px track.
- Bulk state: selecting one account reveals the selected count, cross-page note, clear action, and archive action.
- Archive confirmation: lists the selected account and explains history retention and in-use protection; the final archive action was not submitted during QA.
- Auth methods: `完整 Cookie` and `Cookie + 密码恢复` are separate radio buttons; the latter reveals a required password and requires an email before submit.
- Cookie input: the file control is presented as a dedicated upload surface with ready and replace states.

## Required fidelity surfaces

- Fonts and typography: existing Inter/Segoe UI stack and compact operations hierarchy are preserved; no clipped labels observed.
- Spacing and layout rhythm: fixed table tracks, aligned action buttons, compact filter workspace, and separated form sections are consistent.
- Colors and visual tokens: existing dark neutral, cyan status, orange action, green success, and red destructive tokens are reused.
- Image quality and asset fidelity: this operational screen has no raster imagery; all controls use the existing Lucide icon system.
- Copy and content: archive semantics, cross-page selection, provider status, primary credentials, and auxiliary recovery credentials are explicit.

## Comparison history

1. P1: `.table` resolved to native `display: table`, so a 1320 px account grid expanded the entire 1280 px page.
   - Fix: force `.account-table` to `display: block`, `max-width: 100%`, and `overflow-x: auto`.
   - Post-fix evidence: document width 1265 px at a 1280 px viewport; page overflow false; table viewport 924 px, scroll width 1320 px.
2. P2: mobile search box inherited `flex-basis: 320px`, creating a 584 px filter block before the account list.
   - Fix: at 720 px and below, set the search box to `flex: 0 0 auto`, full width, and 42 px minimum height.
   - Post-fix evidence: production CSS rebuilt with the mobile override; TypeScript and Vite production build passed.

## Findings

- No actionable P0, P1, or P2 issue remains in the requested account-pool flow.
- P3: 1280 px keeps the dense table horizontally scrollable by design; wider desktop viewports show more columns at once.

## Interactions tested

- Provider, status, and role filters rendered with the correct options.
- Row selection and bulk action visibility.
- Archive dialog open and cancel; final destructive submission intentionally skipped.
- Add-account sheet open.
- Leonardo auth method switch and conditional password field.
- Desktop and mobile responsive behavior.
- Browser console contained only an unrelated Statsig network timeout from the Codex host environment.

final result: passed
