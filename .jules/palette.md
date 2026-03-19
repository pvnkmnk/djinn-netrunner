## 2026-03-18 - [Accessibility: ARIA Live Regions and Labels]
**Learning:** Adding `aria-live="polite"` to log consoles and status regions significantly improves the experience for screen reader users by announcing dynamic updates without interrupting their current task. Using a visually hidden `status-announcer` live region is a robust pattern for transient feedback like "Copied to clipboard".
**Action:** Always include ARIA live regions for dynamic content and provide descriptive `aria-label` attributes for icon-only or short-text buttons in future UX tasks.
