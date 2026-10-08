import { matchPath } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { APPLICATION_TABS, ROUTES } from './routes'
describe('application routing contract', () => {
 it('matches application details by ID', () => { expect(matchPath(ROUTES.application, '/apps/application-1')?.params.id).toBe('application-1') })
 it('uses the canonical /apps route', () => { expect(ROUTES.applications).toBe('/apps') })
 it('exposes every hosting section', () => { expect(APPLICATION_TABS).toEqual(['Overview','Runtime','Environment','Ports','Domains','Logs','Jobs','Docker','Settings','Services']) })
})
