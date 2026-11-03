import { fireEvent, render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import { Sidebar, SidebarProvider } from '@/components/ui/sidebar'

import { Header } from '../components/header'

function renderShell() {
  return render(
    <SidebarProvider>
      <Header>
        <span>Workspace title</span>
      </Header>
      <Sidebar />
    </SidebarProvider>
  )
}

test('the top bar is a banner landmark that keeps its content', () => {
  renderShell()
  expect(screen.getByRole('banner')).toHaveTextContent('Workspace title')
})

test('the top bar toggle collapses the sidebar and restores it', () => {
  const { container } = renderShell()
  const sidebar = container.querySelector('[data-slot="sidebar"]')
  expect(sidebar).toHaveAttribute('data-state', 'expanded')

  const toggle = screen.getByRole('button', { name: 'Toggle Sidebar' })
  fireEvent.click(toggle)
  expect(sidebar).toHaveAttribute('data-state', 'collapsed')

  fireEvent.click(toggle)
  expect(sidebar).toHaveAttribute('data-state', 'expanded')
})
