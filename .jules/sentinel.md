# Sentinel Security Journal 🛡️

## 2026-03-17 - Role Mass Assignment in Registration
**Vulnerability:** The registration endpoint (`/api/auth/register`) accepted a `role` field from the user, allowing new accounts to claim `admin` privileges by simply including `"role": "admin"` in the JSON payload.
**Learning:** Fiber's `BodyParser` (or similar deserialization) will map all fields from the request to the struct, and if that struct is used to populate a database model without filtering, sensitive fields can be overwritten by user input.
**Prevention:** Always use a hardcoded value or a safe subset of fields for sensitive properties like `role`. Never trust the request body to define authorization levels.
