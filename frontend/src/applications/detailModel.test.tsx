import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ApplicationRuntime } from './ApplicationTopology'
import { activeRuntimeMetadata } from './detailModel'
import type { ApplicationDetail } from './model'

const app: ApplicationDetail = {
  id: 'test', name: 'App', slug: 'app', description: '', source_type: 'local', driver: 'managed',
  source: { local_path: '/home/developer/app' }, desired_state: 'running', observed_state: 'running', health_state: 'healthy',
  auto_start: false, status: 'running', created_at: '', updated_at: '',
  runtime: { name: 'php', version: '8.3', metadata: { adapter: 'managed' } },
  workloads: [], endpoints: [], deployments: [],
}
describe('deployed runtime details', () => {
  it('does not show a failed replacement plan as the running runtime', () => {
    const deployments = [
      { id: 'failed', driver: 'managed', status: 'failed', stage: 'HEALTH', created_at: '', plan_snapshot: { runtime: { metadata: { generated: false, network: 'failed-network' } } } },
      { id: 'other-driver', driver: 'compose', status: 'success', stage: 'SUCCESS', created_at: '', plan_snapshot: { runtime: { metadata: { network: 'compose-network' } } } },
      { id: 'deployed', driver: 'managed', status: 'success', stage: 'SUCCESS', created_at: '', plan_snapshot: { runtime: { metadata: { generated: true, network: 'active-network' } } } },
    ]
    expect(activeRuntimeMetadata({ ...app, deployments })).toEqual({ adapter: 'managed', generated: true, network: 'active-network' })
    expect(activeRuntimeMetadata({ ...app, deployments, runtime: { name: 'php', metadata: { generated: false } } }).generated).toBe(false)
  })
  it('explains missing generated/mount metadata without claiming the feature is disabled', () => {
    const markup = renderToStaticMarkup(<ApplicationRuntime app={app} editable={false} />)
    expect(markup).toContain('Brak informacji w zapisanym runtime')
    expect(markup).toContain('nie zawiera informacji o mountach')
    expect(markup).toContain('PHP 8.3')
  })
})
