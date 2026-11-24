export default {
  "twoFA": {
  "title": "2FA login and import",
  "label": "Initial login with email, password and 2FA",
  "hint": "One entry per line, two formats supported: email----GPT password----2FA, or email----mailbox password----client_id----refresh_token----GPT password----2FA (when pasting a full mailbox list, the last two fields are used as login password and 2FA). Up to 100 entries. Initial login uses the configured login service. Imported accounts join Credential Operations by default with encrypted password/2FA storage, inspection and automatic re-login enabled. Subsequent logins use the local Worker. Enable credential encryption on this page; automatic re-login also requires a configured Worker. Processing at most 3 accounts at a time. Stopping aborts page requests and further processing; remote logins or saves already submitted may still complete.",
  "settings": "View relogin service settings",
  "credentials": "Login credentials",
  "placeholder": "email----GPT password----2FA\nor: email----mailbox password----client_id----refresh_token----GPT password----2FA",
  "nameHint": "Leave the name empty to use the email; otherwise it is used as a prefix. Click Next to enter login credentials.",
  "invalid": "Enter 1–100 complete email, password and 2FA entries, one per line with no duplicate emails.",
  "start": "Log in and import",
  "retry": "Continue unfinished entries",
  "stop": "Stop this batch",
  "ownedJobs": "Some login tasks are unresolved, so this window must stay open. Continue unfinished entries to resume them, or stop this batch to clean them up. If cleanup fails, you can try again.",
  "reset": "Import another batch",
  "states": {
    "pending": "Pending",
    "login": "Logging in…",
    "importing": "Importing and enrolling in Credential Operations…",
    "created": "Imported and enrolled in Credential Operations",
    "skipped": "Existing account enrolled in Credential Operations",
    "failed": "Login failed",
    "importFailed": "Logged in; import or Credential Operations enrollment incomplete. Check encryption settings and retry"
  },
  "errors": {
    "account_deactivated": "Banned (account deactivated)",
    "account_deleted": "Banned (account deleted)",
    "account_retired": "Banned (account deleted or deactivated)",
    "phone_verification_required": "Phone verification required",
    "requires_relogin": "Credential invalid; login again",
    "password_rejected": "Password rejected",
    "mfa_required": "MFA / verification code required",
    "security_check": "Security check required",
    "authorization_denied": "Authorization denied",
    "rate_limited": "Upstream rate limited",
    "timeout": "Request timed out",
    "transport_error": "Network error",
    "service_error": "Upstream service error",
    "unknown": "Login failed; reason unknown"
  },
  "addedToAccounts": "Added to accounts (error status)"
}
}
