## 2026-03-18 - [Accessibility: ARIA Live Regions and Labels]
**Learning:** Adding `aria-live="polite"` to log consoles and status regions significantly improves the experience for screen reader users by announcing dynamic updates without interrupting their current task. Using a visually hidden `status-announcer` live region is a robust pattern for transient feedback like "Copied to clipboard".
**Action:** Always include ARIA live regions for dynamic content and provide descriptive `aria-label` attributes for icon-only or short-text buttons in future UX tasks.

## 2026-03-26 - [UX: Visual Feedback for HTMX Interactions]
**Learning:** Providing immediate visual feedback for HTMX-driven actions (like Sync or Scan) using the `.htmx-request` class in CSS is an effective, low-effort way to indicate background processing. Combining `opacity` and `cursor: wait` on the triggering element prevents users from wondering if their click was registered.
**Action:** Utilize the `.htmx-request` class in `styles.css` for consistent loading state feedback across all asynchronous UI interactions.

## 2026-04-09 - [UX: Card Focus States for Keyboard Navigation]
**Learning:** Adding `:focus-within` to container cards that house interactive elements (like action buttons) provides essential visual feedback for keyboard users. It ensures that the "current" card is highlighted even if the focus is on a small button inside it, matching the hover behavior.
**Action:** Always pair `:hover` styles with `:focus-within` for interactive card components to maintain consistent feedback across different input methods.
