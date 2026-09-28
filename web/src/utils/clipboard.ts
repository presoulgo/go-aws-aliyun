import { ElMessage } from 'element-plus'

/** 复制文本；非安全上下文（http 内网部署）下退回 execCommand。 */
export async function copyText(text: string, done = '已复制'): Promise<void> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
    } else {
      const ta = document.createElement('textarea')
      ta.value = text
      ta.setAttribute('readonly', '')
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      const ok = document.execCommand('copy')
      ta.remove()
      if (!ok) throw new Error('copy failed')
    }
    ElMessage.success(done)
  } catch {
    ElMessage.error('复制失败，请手动选择文本复制')
  }
}
