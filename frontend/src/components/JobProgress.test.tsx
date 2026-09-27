import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { Job } from '../api/types'
import { JobProgress } from './JobProgress'

describe('JobProgress', () => {
  it('renders stage, numeric progress, elapsed time and log output without a spinner', () => {
    const job: Job = {
      id: 'job-1',
      type: 'deploy',
      status: 'running',
      result: { stage: 'build', progress: 42 },
      created_at: '2026-09-27T18:00:00Z',
      started_at: '2026-09-27T18:00:10Z'
    }
    const markup = renderToStaticMarkup(
      <JobProgress
        job={job}
        now={new Date('2026-09-27T18:01:15Z')}
        logs={[{ cursor: 1, id: '1', source: 'job', level: 'info', message: 'building image', created_at: '2026-09-27T18:00:30Z' }]}
      />
    )
    expect(markup).toContain('build')
    expect(markup).toContain('42%')
    expect(markup).toContain('1m 5s')
    expect(markup).toContain('building image')
    expect(markup.toLowerCase()).not.toContain('spinner')
  })
})
