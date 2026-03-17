# Palette's Journal - NetRunner UX & Accessibility

## 2026-03-17 - [Log Streaming Accessibility]
**Learning:** Log streaming components generated on the backend often overlook basic accessibility features like semantic time elements and ARIA labels, making them difficult for screen reader users to navigate.
**Action:** Always use semantic `<time>` elements with `datetime` attributes for timestamps and provide `aria-label` for status indicators or log levels in backend-generated HTML fragments.
