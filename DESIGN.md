# KeiPay console design

The admin console is an operations tool for payments: calm, dense, and easy to
trust. Think of a payment provider's dashboard, not a landing page. This file
is the contract for every screen. When a feature needs something that is not
here, add it here first.

Code lives in `frontend/`. Tokens are in `frontend/src/app/styles.css`, shared
components in `frontend/src/shared/ui`, and every user-facing string in
`frontend/src/shared/lib/strings.ts`.

## Stack

React 19, Vite, TypeScript (strict), Tailwind v4, shadcn/ui (new-york, Radix,
CSS variables), lucide icons, IBM Plex Sans and IBM Plex Mono (self-hosted via
`@fontsource`, no Google Fonts request), react-router 7, TanStack Query and
Table, react-hook-form with zod, sonner, cmdk, openapi-fetch with types
generated from `api/openapi.yaml`. Do not add another UI or state library
without recording the reason in `docs/design.md`.

## Tokens

All colours are CSS variables in `oklch`, defined for light (`:root`) and dark
(`.dark`) and exposed to Tailwind through `@theme inline`. Components use the
Tailwind utilities (`bg-card`, `text-muted-foreground`, `border-danger-border`),
never raw hex, rgb or Tailwind palette colours such as `text-red-600`.

### Colour

| Token | Use |
|---|---|
| `background` / `foreground` | Page background and body text. |
| `card` / `card-foreground` | Tables, cards, inputs. |
| `popover` / `popover-foreground` | Dialogs, menus, tooltips. |
| `primary` / `primary-foreground` | The one main action per view; active filter chip. |
| `secondary`, `accent`, `muted` (+ `-foreground`) | Hover, quiet surfaces, helper text. `muted-foreground` is the lightest text allowed. |
| `destructive` / `destructive-foreground` | Buttons that rotate, revoke or delete. |
| `border`, `input`, `ring` | Dividers; form control borders (3:1 against `card`); focus ring. |
| `link` | Inline links. |
| `overlay` | The scrim behind dialogs and the mobile sheet. |
| `sidebar`, `sidebar-foreground`, `sidebar-accent`, `sidebar-accent-foreground`, `sidebar-border` | The navigation rail and mobile sheet. |

Status tones. Each has a tinted background, a text colour and a border:

| Tone | Tokens | Used for |
|---|---|---|
| success | `success`, `success-foreground`, `success-border` | `PAID`, `DELIVERED`, active |
| warning | `warning`, `warning-foreground`, `warning-border` | `PENDING` |
| danger | `danger`, `danger-foreground`, `danger-border` | `FAILED`, errors, field messages |
| info | `info`, `info-foreground`, `info-border` | `SETTLED` |
| neutral | `neutral`, `neutral-foreground`, `neutral-border` | `EXPIRED`, inactive, anything unknown |

Test mode: `test-mode`, `test-mode-foreground`, `test-mode-border` (amber). Use
them only for the test-mode banner and switch.

Every text/background pair above is at least 4.5:1 in both themes (body text is
about 17:1, status badges 6:1 or more), and control borders and the focus ring
are at least 3:1. Recheck contrast when you change a token.

### Typography

- Sans: IBM Plex Sans 400/500/600. Mono: IBM Plex Mono 400/500 for ids, keys,
  slugs, event names and JSON.
- Base size 14px (`text-sm`, line height 20px). Inputs use 16px below 768px so
  iOS does not zoom.
- Scale: page title `text-xl font-semibold` (one `h1` per page); section title
  `text-base font-semibold` (`h2`); body `text-sm`; helper and table headers
  `text-xs`. Nothing smaller than 12px.
- Numbers that line up (amounts, counts, attempts) use `tabular-nums`.

### Spacing, radius, shadow

- 4px grid through Tailwind spacing. Page gutter 16px on mobile, 24px from
  768px. Sections stack with `gap-6`; controls inside a section with `gap-3`.
- Radius from `--radius` (8px): `rounded-md` for controls, `rounded-lg` for
  cards and tables, `rounded-full` for badges and chips.
- Shadows stay flat: `shadow-xs` on cards and controls, `shadow-md` on menus,
  `shadow-lg` on dialogs and the mobile sheet.

## Components

Import from `@/shared/ui/*`. Prefer these over new markup.

| Component | Use it for |
|---|---|
| `Button` | Every action. Variants: `default` (one per view), `outline`, `ghost`, `secondary`, `destructive`, `link`. Sizes reach 44px below 768px. |
| `Input`, `Select`, `Checkbox`, `Switch`, `Label` | Form controls. Always with a visible label. |
| `Form` (`FormField`, `FormItem`, `FormLabel`, `FormControl`, `FormDescription`, `FormMessage`) | Every form: react-hook-form + a zod schema that mirrors the contract. Server field errors go on their field; other failures in an `Alert` above the form. |
| `DataTable` | Every list. See the table pattern. |
| `StatusBadge` | Any status. Always text; the tone only reinforces it. |
| `MoneyText` | Every amount. |
| `IdText` | Every id, key, order id or reference. Mono, truncated, full value in `title`, copy button. |
| `DateTimeText` | Every timestamp. Relative text, absolute WIB time in a tooltip. |
| `EmptyState` | No data yet, or a coming-soon feature. Says what goes here and offers the next action. |
| `ErrorState` | A failed load, with a retry button. |
| `Skeleton` | Loading placeholders that match the final layout. |
| `Dialog` | Short focused tasks: forms, details. |
| `ConfirmDialog` | Any mutation the user might regret. Pass `confirmText` (the app slug or resource name) for destructive actions: rotate, revoke, delete. |
| `SecretRevealDialog` | Showing a secret once. Copy buttons, warning, and a required "I have saved this" checkbox before it closes. Escape and outside clicks do not dismiss it. |
| `JsonViewer` | Raw payloads and before/after snapshots. Collapsible, pretty-printed, copyable; long payloads are cut in the view but copied in full. |
| `Timeline` | Ordered events (payment lifecycle, delivery attempts) with time, title, a text status and an optional body. |
| `PageHeader` | The `h1`, one sentence of context, and page actions. |
| `Alert` | Inline messages inside forms and sections. |
| `Tabs` | Sibling views of one resource (Webhooks: Outbound, Inbound). |
| toasts (`sonner`) | Outcome of an action the user just took: "Delivery 7 queued", "Couldn't replay". Never for load errors (use `ErrorState`). |

## Dense table pattern

- `DataTable` with column definitions (`DataColumn`): one column is the card
  title, action columns are `card: "actions"`.
- Filters sit above the table as chips (`filters`: single choice per group,
  clicking the active chip clears it), plus extra controls such as the app
  select in `toolbar`. "Clear filters" appears whenever any filter is set.
- Cursor pagination: pass `pagination={{ nextCursor, onPageChange }}`. The table
  keeps the cursor stack for Previous/Next, disables them at either end, and
  returns to page 1 when a chip changes or `resetKey` changes. Older offset
  lists show "Showing n of total" in `footer`.
- Status is a `StatusBadge`; ids use `IdText`; amounts use `MoneyText`,
  right-aligned with `tabular-nums` in a right-aligned column; times use
  `DateTimeText`.
- Rows are 40px tall, headers `text-xs text-muted-foreground`. Long text
  truncates with the full value in `title`.
- Below 768px the same rows render as cards: title on top, the other fields as
  label/value pairs, actions at the bottom. Never let a table scroll the page
  sideways.
- States: skeleton rows (or cards) while loading; `ErrorState` with retry on
  failure; an `EmptyState` with guidance and an action when empty, and a
  "Clear filters" action when the emptiness comes from filters.

## Detail page pattern

Used from phase 2 on (payment, app, connection details):

1. Header: the resource name or id (`IdText`), its `StatusBadge`, and key facts
   (amount, app, created) as a definition list. Actions on the right.
2. Timeline of what happened, newest last, using `Timeline`.
3. Raw data in collapsed `JsonViewer`s (request, gateway payload, webhook
   body).

Dialogs (such as an audit entry) follow the same order on a smaller scale.

## States

- Loading: skeletons shaped like the content. Buttons that submit show a
  spinner and a verb ("Saving…") and the form's inputs are disabled.
- Empty: say what will appear and how to get it, with an action button
  ("Create app", "View payments"). Coming-soon features use the same component
  and name the feature.
- Error: `ErrorState` with "Try again". Messages come from the error envelope
  `code`; `internal_error`, `network_error` and `unknown_error` get friendly
  copy and never show internal text.
- Session: a 401 `missing_token` or `invalid_token` sends the user to
  `/login?reason=session_expired`, which says "Your session has expired. Sign
  in again."

## Mobile

- Works from 375px wide with a 16px gutter and no horizontal page scroll.
- The sidebar becomes a sheet opened from the menu button in the top bar.
- Touch targets are at least 44px below 768px (buttons, chips, menu items,
  nav links).
- Tables become cards (see above). Dialogs fit the viewport and scroll inside.

## App shell

- Collapsible sidebar (remembered per browser): Dashboard, Apps, Connections,
  Webhooks, Payments, Reconciliation, Settings.
- Top bar: command palette button (⌘K / Ctrl+K), Test/Live switch, theme menu
  (light, dark, system), account menu (change password, sign out).
- Test mode shows an amber banner on every page. Until environments exist, the
  switch only changes the console; the banner must not claim that data is
  filtered.
- The palette navigates to every page, switches theme and mode, and says that
  searching by id or order arrives later.

## Dark mode

The `dark` class on `<html>` swaps every token; components need no `dark:`
variants. The theme is light, dark or system (default), stored in
`localStorage` and applied before the first render. Check every new screen in
both themes.

## Formats

All formatting goes through `frontend/src/shared/lib/format.ts`.

- Money: amounts are integers in minor units with an ISO 4217 code. IDR uses
  exponent 0 (whole rupiah), so `150000 IDR` is `Rp150.000`: no space, `.` for
  thousands, `-Rp2.500` for negatives, `Rp0` for zero. Other currencies use
  their ISO exponent through `Intl.NumberFormat("id-ID")` (`1050 USD` is
  `US$10,50`).
- Numbers: `id-ID` grouping (`1.234`).
- Dates: stored and sent in UTC (RFC 3339), shown in Asia/Jakarta with `id-ID`
  formatting and a `WIB` suffix: `2 Jan 2026, 01.30.00 WIB`. Tables show the
  relative time ("5 minutes ago") with the absolute time in a tooltip and in
  `<time dateTime>`.
- Ids: public ids look like `pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C` and are always
  mono with copy. Numeric ids from older endpoints show as `#12`.
- Missing values show `—`.

## Copy

- Sentence case, short and specific: "Rotate credentials", not "Rotate API
  Credentials Now".
- English today; every string lives in `strings.ts` so another language can be
  added by swapping that object.
- Say what happened and what to do next: "Couldn't replay the delivery." plus a
  retry, not "Error".
- The UI says "App"; the API still says "tenant" until phase 2.

## Accessibility

- Every input has a visible label; icon-only buttons have an `aria-label`.
- Focus is always visible (`focus-visible` ring from the `ring` token).
- Text meets WCAG AA (4.5:1) in both themes; control borders and focus rings
  3:1.
- Status is never colour alone: badges carry text, timeline items carry a
  status label, errors carry a message.
- Dialogs trap focus and have a title and description; destructive confirms
  need typed confirmation.
- One `h1` per page, sections use `h2`.
