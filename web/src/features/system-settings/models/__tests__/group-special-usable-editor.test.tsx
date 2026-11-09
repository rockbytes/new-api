import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { GroupSpecialUsableRulesEditor } from '../group-special-usable-editor'

test('saves only extra visibility grants and drops legacy removal rules when a description changes', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(
    <GroupSpecialUsableRulesEditor
      value='{"vip":{"+:default":"Standard","-:svip":"remove"}}'
      groupOptions={['default', 'vip', 'svip']}
      onChange={onChange}
    />
  )

  await user.type(screen.getByRole('textbox', { name: 'Description' }), '!')

  expect(JSON.parse(onChange.mock.calls.at(-1)?.[0] ?? '{}')).toEqual({
    vip: { '+:default': 'Standard!' },
  })
})
