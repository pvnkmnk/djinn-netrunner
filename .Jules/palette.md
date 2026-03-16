## 2025-03-16 - [Accessibility & Date Standardization]
**Learning:** Standardizing date formats to 'Jan 02 15:04' improves cross-cultural readability compared to 'MM/DD'. Semantic HTML in logs (e.g., <time> and aria-label) significantly enhances screen reader usability for console-first UIs.
**Action:** Use <time> with RFC3339 datetime attributes for all temporal data in server-rendered templates.
