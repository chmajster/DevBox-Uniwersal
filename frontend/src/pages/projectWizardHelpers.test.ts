import { describe, expect, it } from 'vitest'
import { repositoryNameFromURL } from './projectWizardHelpers'

describe('repositoryNameFromURL', () => {
  it('derives a project name from HTTPS Git URLs', () => {
    expect(repositoryNameFromURL('https://github.com/chmajster/OpenWiki.git')).toBe('OpenWiki')
    expect(repositoryNameFromURL('https://gitlab.example.com/team/app')).toBe('app')
  })

  it('supports SSH/scp style repository URLs', () => {
    expect(repositoryNameFromURL('git@github.com:chmajster/DevBox-Uniwersal.git')).toBe('DevBox-Uniwersal')
  })

  it('handles whitespace, trailing slash and invalid input safely', () => {
    expect(repositoryNameFromURL(' https://example.com/team/demo.git/ ')).toBe('demo')
    expect(repositoryNameFromURL('')).toBe('')
  })
})
