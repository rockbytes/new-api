/**
 * Page-level customization left after the colour preset, font, radius and
 * density options were removed. The content layout clamps page content to a
 * readable width on large screens.
 */
export type ContentLayout = 'full' | 'centered'

export type ThemeCustomization = {
  contentLayout: ContentLayout
}

export const DEFAULT_THEME_CUSTOMIZATION: ThemeCustomization = {
  contentLayout: 'full',
}

export const CONTENT_LAYOUT_VALUES: ReadonlySet<ContentLayout> = new Set([
  'full',
  'centered',
])
