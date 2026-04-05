## 2026-03-18 - [Accessibility: ARIA Live Regions and Labels]
**Learning:** Adding `aria-live="polite"` to log consoles and status regions significantly improves the experience for screen reader users by announcing dynamic updates without interrupting their current task. Using a visually hidden `status-announcer` live region is a robust pattern for transient feedback like "Copied to clipboard".
**Action:** Always include ARIA live regions for dynamic content and provide descriptive `aria-label` attributes for icon-only or short-text buttons in future UX tasks.

## 2026-03-26 - [UX: Visual Feedback for HTMX Interactions]
**Learning:** Providing immediate visual feedback for HTMX-driven actions (like Sync or Scan) using the `.htmx-request` class in CSS is an effective, low-effort way to indicate background processing. Combining `opacity` and `cursor: wait` on the triggering element prevents users from wondering if their click was registered.
**Action:** Utilize the `.htmx-request` class in `styles.css` for consistent loading state feedback across all asynchronous UI interactions.

## 2026-04-05 - [Accessibility & Safety: Context-Aware Action Buttons]
**Learning:** For destructive or state-toggling actions, using generic labels like "Delete" or "Enable" is insufficient for accessibility. Including the object's name (e.g., "Delete schedule for My Watchlist") in `aria-label` and `hx-confirm` provides critical context for screen reader users and prevents accidental errors.
**Action:** Always include the specific object name in `aria-label` and `hx-confirm` attributes for action buttons to ensure clarity and safety.
