export default {
  "twoFA": {
  "title": "2FA 登录导入",
  "label": "邮箱、密码与 2FA 首次登录",
  "hint": "每行一条，支持两种格式：邮箱----GPT密码----2FA，或 邮箱----邮箱密码----client_id----refresh_token----GPT密码----2FA（整行粘贴邮箱清单时自动取最后两段作为登录密码与 2FA）。最多 100 条。首次登录使用已配置的重登服务。导入后默认加入凭证运营，加密保存密码与 2FA，并开启巡检、自动重登；后续重登由本地 Worker 执行。可在此页面一键启用凭据加密；自动重登需配置 Worker。 同时最多处理 3 个账号。停止会中止页面请求和后续处理，已提交的远端登录或保存操作可能仍会完成。",
  "settings": "查看重登服务配置",
  "credentials": "登录凭据",
  "placeholder": "邮箱----GPT密码----2FA\n或：邮箱----邮箱密码----client_id----refresh_token----GPT密码----2FA",
  "nameHint": "账号名称可留空，默认使用邮箱；填写后作为名称前缀。点击“下一步”填写登录凭据。",
  "invalid": "请填写 1–100 条完整的邮箱、密码和 2FA，每行一条且邮箱不重复。",
  "start": "登录并导入",
  "retry": "继续未完成的记录",
  "stop": "停止本批处理",
  "ownedJobs": "仍有登录任务未确认结束，暂时不能关闭此窗口。可继续未完成的记录以恢复查询，或点击“停止本批处理”清理任务；清理失败后可再次尝试。",
  "reset": "导入另一批",
  "states": {
    "pending": "待处理",
    "login": "登录中…",
    "importing": "导入并登记凭证运营…",
    "created": "已导入并加入凭证运营",
    "skipped": "已有账号，已加入凭证运营",
    "failed": "登录失败",
    "importFailed": "登录成功，导入或凭证运营登记未完成；可重试，请检查加密配置"
  },
  "errors": {
    "account_deactivated": "封号",
    "account_deleted": "封号（账号已删除）",
    "account_retired": "封号（账号已删除或停用）",
    "phone_verification_required": "需要手机号验证",
    "requires_relogin": "凭据失效，需重新登录",
    "password_rejected": "密码错误或被拒绝",
    "mfa_required": "需要 MFA / 验证码",
    "security_check": "需要安全验证",
    "authorization_denied": "授权被拒绝",
    "rate_limited": "上游限流",
    "timeout": "请求超时",
    "transport_error": "网络连接异常",
    "service_error": "上游服务异常",
    "unknown": "登录失败，原因未确认"
  },
  "addedToAccounts": "已加入账号列表（错误状态）"
}
}
