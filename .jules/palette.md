## 2026-03-18 - [Accessibility: ARIA Live Regions and Labels]
**Learning:** Adding `aria-live="polite"` to log consoles and status regions significantly improves the experience for screen reader users by announcing dynamic updates without interrupting their current task. Using a visually hidden `status-announcer` live region is a robust pattern for transient feedback like "Copied to clipboard".
**Action:** Always include ARIA live regions for dynamic content and provide descriptive `aria-label` attributes for icon-only or short-text buttons in future UX tasks.

## 2026-03-27 - [Accessibility: Specific ARIA Labels and Toggles]
**Learning:** For interactive components like toggle switches, placing the `aria-label` directly on the `<input>` element instead of a wrapper `<label>` ensures more reliable announcement by screen readers. Additionally, including the object's name in ARIA labels (e.g., "Sync watchlist [Name]") provides essential context that is lost when using generic labels like "Sync" in a list.
**Action:** Apply `aria-label` to the specific input element in custom controls and always interpolate item names into action labels for list-based UI components.
