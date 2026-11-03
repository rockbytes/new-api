// Maps backend system task type constants to i18n source keys. Unknown/future
// types fall back to their raw identifier so the panel never shows blank.
export const SYSTEM_TASK_TYPE_LABEL: Record<string, string> = {
  log_cleanup: 'Log cleanup',
  channel_test: 'Batch channel test',
  model_update: 'Batch upstream model update',
  midjourney_poll: 'Drawing task polling',
  async_task_poll: 'Async task polling',
}
