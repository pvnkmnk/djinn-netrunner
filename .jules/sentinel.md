## 2026-03-16 - Mass Assignment in User Registration
**Vulnerability:** The registration endpoint allowed users to specify their own `role`, enabling privilege escalation by setting `role: "admin"`.
**Learning:** Fiber's `BodyParser` will map any matching JSON field to the struct, even if it's a sensitive field like `role`.
**Prevention:** Explicitly set sensitive fields in the backend logic instead of relying on client-provided values. Use separate structs for input validation vs. database models.
