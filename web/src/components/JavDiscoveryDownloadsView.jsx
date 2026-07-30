import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  cancelDiscoveryDownload,
  deleteDiscoveryDownload,
  fetchDiscoveryDownloads,
  fetchDownloaderSettings,
  fetchDirectories,
  retryDiscoveryDownload,
  testDownloaderProvider,
  updateDownloaderProvider,
  updateDownloaderSettings,
} from '@/api'
import { getErrorMessage } from '@/utils/errors'
import { zh } from '@/utils/i18n'

const statusLabels = {
  queued: ['等待处理', 'Queued'],
  offline_downloading: ['云端离线中', 'Offline downloading'],
  resolving_files: ['正在解析文件', 'Resolving files'],
  waiting_local_download: ['等待本地下载', 'Waiting for local download'],
  local_downloading: ['正在下载到本地', 'Downloading locally'],
  completed: ['已完成', 'Completed'],
  failed: ['失败', 'Failed'],
  canceled: ['已取消', 'Canceled'],
}

const activeStatuses = new Set([
  'queued',
  'offline_downloading',
  'resolving_files',
  'waiting_local_download',
  'local_downloading',
])

const providerLabels = {
  clouddrive2: 'CloudDrive2',
  openlist: 'OpenList',
}

const defaultProviderForms = {
  clouddrive2: { address: 'http://127.0.0.1:19798', apiToken: '', remoteFolder: '' },
  openlist: { address: 'http://127.0.0.1:5244', apiToken: '', remoteFolder: '' },
}

function formatBytes(value) {
  let bytes = Number(value)
  if (!Number.isFinite(bytes) || bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let index = 0
  while (bytes >= 1024 && index < units.length - 1) {
    bytes /= 1024
    index += 1
  }
  return `${bytes.toFixed(index >= 3 ? 2 : 1)} ${units[index]}`
}

function formatTime(value) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function statusLabel(status) {
  const labels = statusLabels[status] || [status, status]
  return zh(labels[0], labels[1])
}

export default function JavDiscoveryDownloadsView() {
  const [settings, setSettings] = useState(null)
  const [directories, setDirectories] = useState([])
  const [jobs, setJobs] = useState([])
  const [form, setForm] = useState({
    activeProvider: '',
    directoryId: '',
    localConcurrency: 2,
  })
  const [providerForms, setProviderForms] = useState(defaultProviderForms)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [busyJobIds, setBusyJobIds] = useState(() => new Set())
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const loadSettings = useCallback(async () => {
    const [nextSettings, directoryItems] = await Promise.all([
      fetchDownloaderSettings(),
      fetchDirectories(),
    ])
    setSettings(nextSettings)
    setDirectories(
      (Array.isArray(directoryItems) ? directoryItems : []).filter(
        (directory) => !directory.is_delete
      )
    )
    setForm({
      activeProvider: nextSettings?.active_provider || '',
      directoryId: nextSettings?.directory_id ? String(nextSettings.directory_id) : '',
      localConcurrency: Number(nextSettings?.local_concurrency) || 2,
    })
    setProviderForms(
      Object.fromEntries(
        Object.entries(defaultProviderForms).map(([provider, defaults]) => {
          const saved = nextSettings?.providers?.[provider]
          return [
            provider,
            {
              address: saved?.address || defaults.address,
              apiToken: '',
              remoteFolder: saved?.remote_folder || '',
            },
          ]
        })
      )
    )
  }, [])

  const loadJobs = useCallback(async () => {
    const payload = await fetchDiscoveryDownloads()
    setJobs(Array.isArray(payload?.items) ? payload.items : [])
  }, [])

  useEffect(() => {
    let cancelled = false
    Promise.all([loadSettings(), loadJobs()])
      .catch((loadError) => {
        if (!cancelled) setError(getErrorMessage(loadError))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [loadJobs, loadSettings])

  useEffect(() => {
    const timer = window.setInterval(() => {
      loadJobs().catch(() => {})
    }, 3000)
    return () => window.clearInterval(timer)
  }, [loadJobs])

  const counts = useMemo(() => {
    let active = 0
    let failed = 0
    let completed = 0
    jobs.forEach((job) => {
      if (activeStatuses.has(job.status)) active += 1
      else if (job.status === 'failed') failed += 1
      else if (job.status === 'completed') completed += 1
    })
    return { active, failed, completed }
  }, [jobs])

  const updateProviderForm = (provider, field, value) => {
    setProviderForms((current) => ({
      ...current,
      [provider]: { ...current[provider], [field]: value },
    }))
  }

  const saveSelectedProvider = async () => {
    const provider = form.activeProvider
    if (!provider) return
    const providerForm = providerForms[provider]
    const payload = {
      address: providerForm.address.trim(),
      remote_folder: providerForm.remoteFolder.trim(),
    }
    if (providerForm.apiToken.trim()) payload.api_token = providerForm.apiToken.trim()
    await updateDownloaderProvider(provider, payload)
    setProviderForms((current) => ({
      ...current,
      [provider]: { ...current[provider], apiToken: '' },
    }))
  }

  const saveCommonSettings = async () => {
    const saved = await updateDownloaderSettings({
      active_provider: form.activeProvider,
      directory_id: form.directoryId ? Number(form.directoryId) : null,
      local_concurrency: Number(form.localConcurrency),
    })
    setSettings(saved)
  }

  const handleSave = async (event) => {
    event.preventDefault()
    setSaving(true)
    setError('')
    setNotice('')
    try {
      await saveSelectedProvider()
      await saveCommonSettings()
      setNotice(zh('下载器配置已保存', 'Downloader settings saved'))
    } catch (saveError) {
      setError(getErrorMessage(saveError))
    } finally {
      setSaving(false)
    }
  }

  const handleTest = async () => {
    setTesting(true)
    setError('')
    setNotice('')
    try {
      if (!form.activeProvider) throw new Error(zh('请先选择下载器', 'Select a downloader first'))
      await saveSelectedProvider()
      const result = await testDownloaderProvider(form.activeProvider)
      await saveCommonSettings()
      setNotice(
        zh(
          `连接成功：${result?.user_name || providerLabels[form.activeProvider]}，目标目录可用`,
          `Connected as ${result?.user_name || providerLabels[form.activeProvider]}; the target folder is ready`
        )
      )
    } catch (testError) {
      setError(getErrorMessage(testError))
    } finally {
      setTesting(false)
    }
  }

  const runJobAction = async (job, action) => {
    setBusyJobIds((current) => new Set(current).add(job.id))
    setError('')
    try {
      await action(job.id)
      await loadJobs()
    } catch (actionError) {
      setError(getErrorMessage(actionError))
    } finally {
      setBusyJobIds((current) => {
        const next = new Set(current)
        next.delete(job.id)
        return next
      })
    }
  }

  const selectedProviderForm = providerForms[form.activeProvider]
  const selectedProviderSettings = settings?.providers?.[form.activeProvider]

  return (
    <div className="space-y-5">
      {error ? (
        <div role="alert" className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      ) : null}
      {notice ? (
        <div role="status" className="rounded-lg bg-green-50 px-3 py-2 text-sm text-green-700">
          {notice}
        </div>
      ) : null}

      <section className="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="text-xl font-bold text-gray-900">
              {zh('发现页下载器设置', 'Discovery downloader settings')}
            </h2>
            <p className="mt-1 text-sm text-gray-500">
              {zh(
                '磁力先离线到云盘，再由 JavBoss 下载到所选本地媒体目录。',
                'Magnets are transferred to cloud storage first, then downloaded into the selected local media directory.'
              )}
            </p>
          </div>
          <span
            className={`rounded-full px-2.5 py-1 text-xs font-semibold ${
              settings?.active_provider
                ? 'bg-emerald-100 text-emerald-700'
                : 'bg-gray-100 text-gray-500'
            }`}
          >
            {settings?.active_provider
              ? `${zh('已启用', 'Enabled')} · ${providerLabels[settings.active_provider]}`
              : zh('未启用', 'Disabled')}
          </span>
        </div>

        <form onSubmit={handleSave} className="mt-5 grid gap-3 lg:grid-cols-2">
          <label className="text-xs font-medium text-gray-600">
            {zh('激活下载器', 'Active downloader')}
            <select
              value={form.activeProvider}
              onChange={(event) =>
                setForm((current) => ({ ...current, activeProvider: event.target.value }))
              }
              className="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-white px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
            >
              <option value="">{zh('不启用下载器', 'Disable downloader')}</option>
              <option value="clouddrive2">CloudDrive2</option>
              <option value="openlist">OpenList · 115 Open</option>
            </select>
            <span className="mt-1 block font-normal text-gray-400">
              {zh(
                '一次只能激活一个下载器；队列存在未结束任务时不能切换。',
                'Only one downloader can be active; switching is blocked while jobs are unfinished.'
              )}
            </span>
          </label>
          <label className="text-xs font-medium text-gray-600">
            {zh('本地下载目录', 'Local download directory')}
            <select
              value={form.directoryId}
              onChange={(event) =>
                setForm((current) => ({ ...current, directoryId: event.target.value }))
              }
              className="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-white px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
            >
              <option value="">{zh('请选择目录', 'Select a directory')}</option>
              {directories.map((directory) => (
                <option key={directory.id} value={String(directory.id)}>
                  {directory.path}
                </option>
              ))}
            </select>
          </label>
          <label className="text-xs font-medium text-gray-600">
            {zh('本地下载并发数', 'Concurrent local downloads')}
            <select
              value={form.localConcurrency}
              onChange={(event) =>
                setForm((current) => ({
                  ...current,
                  localConcurrency: Number(event.target.value),
                }))
              }
              className="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-white px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
            >
              {[1, 2, 3, 4, 5].map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
            <span className="mt-1 block font-normal text-gray-400">
              {zh(
                '云端离线任务可并行；此项仅限制同时写入本地的任务数。',
                'Cloud offline jobs run in parallel; this only limits simultaneous local transfers.'
              )}
            </span>
          </label>

          {selectedProviderForm ? (
            <div className="grid gap-3 rounded-xl border border-gray-200 bg-gray-50 p-4 lg:col-span-2 lg:grid-cols-2">
              <label className="text-xs font-medium text-gray-600">
                {providerLabels[form.activeProvider]} {zh('地址', 'address')}
                <input
                  value={selectedProviderForm.address}
                  onChange={(event) =>
                    updateProviderForm(form.activeProvider, 'address', event.target.value)
                  }
                  placeholder={defaultProviderForms[form.activeProvider].address}
                  className="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-white px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                />
              </label>
              <label className="text-xs font-medium text-gray-600">
                {form.activeProvider === 'openlist'
                  ? zh('管理员 API Token', 'Admin API token')
                  : zh('API Token', 'API token')}
                <input
                  type="password"
                  value={selectedProviderForm.apiToken}
                  onChange={(event) =>
                    updateProviderForm(form.activeProvider, 'apiToken', event.target.value)
                  }
                  placeholder={
                    selectedProviderSettings?.token_configured
                      ? zh('已配置；留空保持不变', 'Configured; leave blank to keep it')
                      : zh('请输入 API Token', 'Enter an API token')
                  }
                  autoComplete="new-password"
                  className="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-white px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                />
              </label>
              <label className="text-xs font-medium text-gray-600 lg:col-span-2">
                {form.activeProvider === 'openlist'
                  ? zh('115 Open 目标目录', '115 Open target folder')
                  : zh('云端离线目录', 'Remote offline folder')}
                <input
                  value={selectedProviderForm.remoteFolder}
                  onChange={(event) =>
                    updateProviderForm(form.activeProvider, 'remoteFolder', event.target.value)
                  }
                  placeholder="/115/JavBoss"
                  className="mt-1 h-10 w-full rounded-lg border border-gray-300 bg-white px-3 text-sm outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-100"
                />
              </label>
              {form.activeProvider === 'openlist' ? (
                <p className="text-xs leading-5 text-amber-700 lg:col-span-2">
                  {zh(
                    '当前只调用 OpenList 的“115 Open”离线工具。请先在 OpenList 管理设置中配置 115 Open 临时目录；该临时目录和这里的目标目录都必须位于已启用的 115 Open 存储中。',
                    'This integration always uses the “115 Open” offline tool. Configure its temporary directory in OpenList first; both that folder and this target folder must be on an enabled 115 Open storage.'
                  )}
                </p>
              ) : null}
            </div>
          ) : null}

          <div className="flex justify-end gap-2 lg:col-span-2">
            <button
              type="button"
              disabled={testing || saving || !form.activeProvider}
              onClick={handleTest}
              className="h-10 rounded-lg border border-blue-200 bg-blue-50 px-4 text-sm font-semibold text-blue-700 hover:bg-blue-100 disabled:opacity-50"
            >
              {testing ? zh('测试中…', 'Testing...') : zh('保存并测试', 'Save and test')}
            </button>
            <button
              type="submit"
              disabled={saving || testing}
              className="h-10 rounded-lg bg-blue-600 px-4 text-sm font-semibold text-white hover:bg-blue-700 disabled:opacity-50"
            >
              {saving ? zh('保存中…', 'Saving...') : zh('保存设置', 'Save settings')}
            </button>
          </div>
        </form>
      </section>

      <section>
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="text-2xl font-bold text-gray-900">{zh('下载队列', 'Download queue')}</h2>
            <p className="mt-1 text-sm text-gray-500">
              {zh(
                `进行中 ${counts.active} · 已完成 ${counts.completed} · 失败 ${counts.failed}`,
                `${counts.active} active · ${counts.completed} completed · ${counts.failed} failed`
              )}
            </p>
          </div>
          <button
            type="button"
            onClick={() => loadJobs().catch((loadError) => setError(getErrorMessage(loadError)))}
            className="rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-600 hover:bg-gray-50"
          >
            {zh('刷新', 'Refresh')}
          </button>
        </div>

        {loading ? (
          <div className="mt-4 rounded-xl border border-dashed border-gray-200 bg-white p-10 text-center text-sm text-gray-400">
            {zh('加载下载队列…', 'Loading download queue...')}
          </div>
        ) : jobs.length === 0 ? (
          <div className="mt-4 rounded-xl border border-dashed border-gray-200 bg-white p-10 text-center text-sm text-gray-400">
            {zh(
              '队列为空，可在作品详情的磁力列表中创建任务',
              'The queue is empty. Add a job from an item’s magnet list.'
            )}
          </div>
        ) : (
          <div className="mt-4 space-y-3">
            {jobs.map((job) => {
              const progress =
                job.status === 'completed'
                  ? 100
                  : job.bytes_total > 0
                    ? Math.max(0, Math.min(100, (job.bytes_downloaded * 100) / job.bytes_total))
                    : 0
              const showLocalProgress = ['local_downloading', 'completed'].includes(job.status)
              const busy = busyJobIds.has(job.id)
              const terminal = ['completed', 'failed', 'canceled'].includes(job.status)
              return (
                <article
                  key={job.id}
                  className="rounded-xl border border-gray-200 bg-white p-4 shadow-sm"
                >
                  <div className="flex flex-wrap items-start gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono text-base font-bold text-gray-900">
                          {job.code}
                        </span>
                        <span
                          className={`rounded-full px-2 py-0.5 text-xs font-semibold ${
                            job.status === 'completed'
                              ? 'bg-emerald-100 text-emerald-700'
                              : job.status === 'failed'
                                ? 'bg-red-100 text-red-700'
                                : job.status === 'canceled'
                                  ? 'bg-gray-100 text-gray-500'
                                  : 'bg-blue-100 text-blue-700'
                          }`}
                        >
                          {statusLabel(job.status)}
                        </span>
                        <span className="rounded-full bg-violet-50 px-2 py-0.5 text-xs font-medium text-violet-700">
                          {providerLabels[job.provider] || job.provider}
                        </span>
                      </div>
                      <div className="mt-1 truncate text-sm text-gray-600" title={job.magnet_name}>
                        {job.magnet_name || job.info_hash}
                      </div>
                      <div
                        className="mt-1 truncate text-xs text-gray-400"
                        title={job.directory_path}
                      >
                        {zh('本地：', 'Local: ')}
                        {job.directory_path} · {formatTime(job.created_at)}
                      </div>
                    </div>
                    <div className="flex shrink-0 gap-2">
                      {['failed', 'canceled'].includes(job.status) ? (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => runJobAction(job, retryDiscoveryDownload)}
                          className="rounded-md border border-blue-200 bg-blue-50 px-3 py-1.5 text-xs font-semibold text-blue-700 disabled:opacity-50"
                        >
                          {zh('重试', 'Retry')}
                        </button>
                      ) : null}
                      {activeStatuses.has(job.status) ? (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => runJobAction(job, cancelDiscoveryDownload)}
                          className="rounded-md border border-amber-200 bg-amber-50 px-3 py-1.5 text-xs font-semibold text-amber-700 disabled:opacity-50"
                        >
                          {zh('取消', 'Cancel')}
                        </button>
                      ) : null}
                      {terminal ? (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => runJobAction(job, deleteDiscoveryDownload)}
                          className="rounded-md border border-gray-200 bg-white px-3 py-1.5 text-xs font-semibold text-gray-600 disabled:opacity-50"
                        >
                          {zh('移除记录', 'Remove')}
                        </button>
                      ) : null}
                    </div>
                  </div>
                  {showLocalProgress ? (
                    <>
                      <div className="mt-3 h-2 overflow-hidden rounded-full bg-gray-100">
                        <div
                          className="h-full rounded-full bg-blue-600 transition-all"
                          style={{ width: `${progress}%` }}
                        />
                      </div>
                      <div className="mt-1 flex justify-between text-xs text-gray-400">
                        <span>{progress.toFixed(1)}%</span>
                        <span>
                          {formatBytes(job.bytes_downloaded)} / {formatBytes(job.bytes_total)}
                        </span>
                      </div>
                    </>
                  ) : null}
                  {job.error_message ? (
                    <div className="mt-2 break-words rounded-md bg-red-50 px-2.5 py-2 text-xs text-red-700">
                      {job.error_message}
                    </div>
                  ) : null}
                  {Array.isArray(job.local_files) && job.local_files.length > 0 ? (
                    <div className="mt-2 text-xs text-emerald-700">
                      {zh(
                        `已保存 ${job.local_files.length} 个视频文件`,
                        `${job.local_files.length} video files saved`
                      )}
                    </div>
                  ) : null}
                </article>
              )
            })}
          </div>
        )}
      </section>
    </div>
  )
}
