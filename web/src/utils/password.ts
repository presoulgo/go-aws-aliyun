import type { FormItemRule } from 'element-plus'

/** 与后端 auth.ValidatePassword 保持一致：10–72 字节，同时包含字母和数字。 */
export function checkPassword(pw: string): string {
  if (pw.length < 10) return '密码至少 10 位'
  if (new TextEncoder().encode(pw).length > 72) return '密码不能超过 72 个字节'
  if (!/[A-Za-z]/.test(pw) || !/[0-9]/.test(pw)) return '密码需同时包含字母和数字'
  return ''
}

export const passwordRule: FormItemRule = {
  validator: (_rule, value: string, callback) => {
    const msg = value ? checkPassword(value) : ''
    if (msg) callback(new Error(msg))
    else callback()
  },
  trigger: 'blur',
}

/** 生成随机密码（字母 + 数字，默认 12 位），保证两类字符都出现。 */
export function generatePassword(length = 12): string {
  const letters = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ'
  const digits = '23456789'
  const all = letters + digits
  const buf = new Uint32Array(length)
  for (;;) {
    crypto.getRandomValues(buf)
    const pw = Array.from(buf, (n) => all[n % all.length]).join('')
    if (/[A-Za-z]/.test(pw) && /[0-9]/.test(pw)) return pw
  }
}
