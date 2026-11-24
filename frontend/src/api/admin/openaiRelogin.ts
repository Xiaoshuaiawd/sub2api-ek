import { apiClient } from '../client'

export interface OpenAIReloginConfig {
  relogin_endpoint: string
  relogin_headers: Record<string, string> | null
}

export async function getReloginConfig(): Promise<OpenAIReloginConfig> {
  return (await apiClient.get('/admin/account-ops/relogin/config')).data
}

export async function saveReloginConfig(config: OpenAIReloginConfig): Promise<OpenAIReloginConfig> {
  return (await apiClient.put('/admin/account-ops/relogin/config', config)).data
}

export interface OpenAIReloginCredentials {
  email: string
  password: string
  mfa_secret: string
}

// Unlike the settings parser, imports must reject malformed lines rather than
// silently dropping an account from a batch.
export function parseTwoFALoginText(raw: string): OpenAIReloginCredentials[] {
  const lines = raw.split(/\r?\n/).map(line => line.trim()).filter(Boolean)
  if (!lines.length || lines.length > 100) throw new Error('invalid_batch')
  const seen = new Set<string>()
  return lines.map(line => {
    const parts = (line.includes('----') ? line.split('----') : line.split(',')).map(part => part.trim())
    // 兼容两种导入格式：
    // 1) 邮箱----GPT密码----2FA
    // 2) 邮箱----邮箱密码----client_id----refresh_token----GPT密码----2FA
    //    整行直接粘贴邮箱清单时，登录只用后两段（GPT 密码 + 2FA），前四段里的邮箱凭据不参与登录。
    let email = ''
    let password = ''
    let mfa = ''
    if (parts.length === 3) {
      [email = '', password = '', mfa = ''] = parts
    } else if (parts.length === 6) {
      email = parts[0] ?? ''
      password = parts[4] ?? ''
      mfa = parts[5] ?? ''
    }
    const normalizedEmail = email.toLowerCase()
    if ((parts.length !== 3 && parts.length !== 6) || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email) ||
        !password || !mfa || seen.has(normalizedEmail)) throw new Error('invalid_batch')
    seen.add(normalizedEmail)
    return { email: normalizedEmail, password, mfa_secret: mfa }
  })
}

export interface TwoFALoginJob {
  id: string
  status: 'running' | 'succeeded' | 'failed'
  credential?: Record<string, unknown>
  // 失败原因短码（已脱敏），例如 account_deactivated 表示封号。
  error_code?: string
  // 失败后登记到账号列表的错误态账号 ID（0/未返回表示未登记）。
  account_id?: number
}

const twoFALoginPath = '/admin/account-ops/relogin/two-fa-login'

export async function startTwoFALogin(entry: OpenAIReloginCredentials, options?: { signal?: AbortSignal }): Promise<TwoFALoginJob> {
  return (await apiClient.post(twoFALoginPath, { ...entry, credential_target: 'operations' }, options)).data
}

export async function getTwoFALogin(id: string, options?: { signal?: AbortSignal }): Promise<TwoFALoginJob> {
  return (await apiClient.get(`${twoFALoginPath}/${id}`, options)).data
}

export async function deleteTwoFALogin(id: string): Promise<void> {
  await apiClient.delete(`${twoFALoginPath}/${id}`)
}
