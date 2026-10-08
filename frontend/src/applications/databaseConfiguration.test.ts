import { beforeEach, describe, expect, it, vi } from 'vitest'
import { request } from '../api/client'
import { applyDatabaseChoice, emptyDatabaseChoice, waitForJob } from './databaseConfiguration'
vi.mock('../api/client', () => ({ request: vi.fn() }))
const mocked = vi.mocked(request)
describe('application database jobs', () => {
 beforeEach(() => mocked.mockReset())
 it('recognizes the Job Engine succeeded status', async () => {
  mocked.mockResolvedValue({ id: 'sql', status: 'succeeded' })
  await expect(waitForJob('sql')).resolves.toMatchObject({ status: 'succeeded' })
  expect(mocked).toHaveBeenCalledOnce()
 })
 it('reports an actual SQL failure', async () => {
  mocked.mockResolvedValue({ id: 'sql', status: 'failed', error: 'access denied' })
  await expect(waitForJob('sql')).rejects.toThrow('access denied')
 })
 it('creates an application account on the selected existing database', async () => {
  mocked.mockResolvedValueOnce({ id: 'provision' }).mockResolvedValueOnce({ id: 'provision', status: 'succeeded' })
  await applyDatabaseChoice('app', { ...emptyDatabaseChoice, mode: 'existing', database_id: 'selected', user_id: 'new', username: 'app_user' })
  expect(mocked.mock.calls[0][0]).toBe('/applications/app/database-binding/provision')
  expect(JSON.parse(String(mocked.mock.calls[0][1]?.body))).toMatchObject({ database_id: 'selected', username: 'app_user' })
 })
})
