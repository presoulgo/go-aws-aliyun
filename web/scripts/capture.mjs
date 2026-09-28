import { chromium } from 'playwright-core'
import { mkdir } from 'node:fs/promises'
import { resolve } from 'node:path'

const base = process.env.OPS_TEST_URL || 'http://localhost:8080'
const password = process.env.OPS_TEST_PASSWORD
if (!password) throw new Error('OPS_TEST_PASSWORD is required')
const output = resolve('../design/screenshots')
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH || 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe' })
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 })
async function capture(name, path) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  await page.screenshot({ path: resolve(output, `${name}.png`), fullPage: true })
  console.log(name, page.url())
}
await capture('01-login', '/login')
await page.getByRole('textbox', { name: '用户名' }).fill('admin')
await page.locator('input[type=password]').fill(password)
await page.getByRole('button', { name: '登录平台' }).click()
await page.waitForURL('**/dashboard')
await capture('02-dashboard', '/dashboard')
await capture('03-resources', '/resources')
await capture('04-resource-detail', '/resources?detail=1')
await page.getByRole('tab', { name: '监控' }).click()
await page.screenshot({ path: resolve(output, '05-resource-metrics.png'), fullPage: true })
await capture('06-monitor', '/monitor?type=vm&metric=cpu&ids=1,365&range=24h')
await capture('07-accounts', '/accounts')
await capture('08-account-dialog', '/accounts?new=1')
await capture('09-users', '/system/users')
await capture('10-audit', '/system/audit')
await browser.close()
