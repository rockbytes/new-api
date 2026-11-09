import {
  AlertTriangle,
  ChevronDown,
  ChevronUp,
  Plus,
  Trash2,
} from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'

const sectionCardClassName = 'min-w-0 shadow-none'
const sectionHeaderClassName = 'gap-2 border-b'

type Rule = {
  _id: string
  userGroup: string
  targetGroup: string
  description: string
}

let _idCounter = 0
function uid() {
  return `gsu_${++_idCounter}`
}

// Only "+:<group>" rules are read: each one grants one group to a user group.
// Any other key (including legacy "-:" removal rules) is dropped on save, so
// the saved configuration matches what the backend honours.
const GRANT_PREFIX = '+:'

function safeParseJson(str: string): Record<string, Record<string, string>> {
  if (!str || !str.trim()) return {}
  try {
    return JSON.parse(str) as Record<string, Record<string, string>>
  } catch {
    return {}
  }
}

function flattenRules(nested: Record<string, Record<string, string>>): Rule[] {
  const rules: Rule[] = []
  for (const [userGroup, inner] of Object.entries(nested)) {
    if (typeof inner !== 'object' || inner === null) continue
    for (const [rawKey, desc] of Object.entries(inner)) {
      if (!rawKey.startsWith(GRANT_PREFIX)) continue
      rules.push({
        _id: uid(),
        userGroup,
        targetGroup: rawKey.slice(GRANT_PREFIX.length),
        description: typeof desc === 'string' ? desc : '',
      })
    }
  }
  return rules
}

function serializeRules(rules: Rule[]): string {
  const result: Record<string, Record<string, string>> = {}
  for (const { userGroup, targetGroup, description } of rules) {
    if (!userGroup || !targetGroup) continue
    if (!result[userGroup]) result[userGroup] = {}
    result[userGroup][`${GRANT_PREFIX}${targetGroup}`] = description
  }
  return Object.keys(result).length === 0
    ? '{}'
    : JSON.stringify(result, null, 2)
}

type GroupSelectProps = {
  options: string[]
  value: string
  placeholder: string
  onValueChange: (value: string) => void
  className?: string
}

function GroupSelect(props: GroupSelectProps) {
  const knownOptions = useMemo(() => {
    if (props.value && !props.options.includes(props.value)) {
      return [props.value, ...props.options]
    }
    return props.options
  }, [props.options, props.value])

  return (
    <Combobox
      options={knownOptions.map((name) => ({ value: name, label: name }))}
      value={props.value}
      onValueChange={(value) => {
        if (value) props.onValueChange(value)
      }}
      className={props.className}
      placeholder={props.placeholder}
      aria-label={props.placeholder}
    />
  )
}

type GroupSpecialUsableRulesEditorProps = {
  value: string
  groupOptions: string[]
  onChange: (value: string) => void
}

type GroupSectionProps = {
  groupName: string
  items: Rule[]
  groupOptions: string[]
  onUpdate: (
    id: string,
    field: 'targetGroup' | 'description',
    val: string
  ) => void
  onRemove: (id: string) => void
  onAdd: (groupName: string) => void
  onRemoveGroup: (groupName: string) => void
}

function GroupSection(props: GroupSectionProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(true)
  const isKnownGroup = props.groupOptions.includes(props.groupName)

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <div className='rounded-lg border'>
        <div className='flex items-center justify-between gap-2 p-3'>
          <CollapsibleTrigger
            render={
              <Button
                variant='ghost'
                className='h-auto min-w-0 justify-start whitespace-normal'
              />
            }
            aria-label={t('Rules for {{group}}', { group: props.groupName })}
          >
            {open ? (
              <ChevronUp className='h-4 w-4' />
            ) : (
              <ChevronDown className='h-4 w-4' />
            )}
            <span className='min-w-0 truncate font-semibold'>
              {props.groupName}
            </span>
            {!isKnownGroup && (
              <StatusBadge variant='danger' copyable={false}>
                <AlertTriangle className='mr-1 h-3 w-3' />
                {t('Not in pricing table')}
              </StatusBadge>
            )}
            <StatusBadge variant='neutral' copyable={false}>
              {props.items.length} {t('rules')}
            </StatusBadge>
          </CollapsibleTrigger>
          <div className='flex shrink-0 items-center gap-1'>
            <Button
              variant='ghost'
              size='sm'
              className='h-7 w-7 p-0'
              aria-label={t('Add rule')}
              onClick={() => {
                props.onAdd(props.groupName)
                setOpen(true)
              }}
            >
              <Plus className='h-4 w-4' />
            </Button>
            <Button
              variant='ghost'
              size='sm'
              className='text-destructive h-7 w-7 p-0'
              aria-label={t('Remove {{group}}', { group: props.groupName })}
              onClick={() => props.onRemoveGroup(props.groupName)}
            >
              <Trash2 className='h-4 w-4' />
            </Button>
          </div>
        </div>
        <CollapsibleContent>
          <div className='space-y-2 border-t p-3'>
            {props.items.map((rule) => (
              <div
                key={rule._id}
                className='grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 rounded-lg border p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:border-0 sm:p-0'
              >
                <div className='flex min-w-0 items-center gap-1.5'>
                  <GroupSelect
                    className='min-w-0 flex-1'
                    options={props.groupOptions}
                    value={rule.targetGroup}
                    placeholder={t('Group name')}
                    onValueChange={(v) =>
                      props.onUpdate(rule._id, 'targetGroup', v)
                    }
                  />
                  {rule.targetGroup &&
                    !props.groupOptions.includes(rule.targetGroup) && (
                      <AlertTriangle
                        className='text-destructive h-4 w-4 shrink-0'
                        aria-label={t('Not in pricing table')}
                      />
                    )}
                </div>
                <Input
                  className='col-start-1 row-start-2 min-w-0 sm:col-start-auto sm:row-start-auto'
                  aria-label={t('Description')}
                  value={rule.description}
                  placeholder={t('Description')}
                  onChange={(e) =>
                    props.onUpdate(rule._id, 'description', e.target.value)
                  }
                />
                <Button
                  variant='ghost'
                  size='sm'
                  className='text-destructive col-start-2 row-start-1 size-8 p-0 sm:col-start-auto sm:row-start-auto'
                  aria-label={t('Delete rule')}
                  onClick={() => props.onRemove(rule._id)}
                >
                  <Trash2 className='h-4 w-4' />
                </Button>
              </div>
            ))}
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}

export function GroupSpecialUsableRulesEditor(
  props: GroupSpecialUsableRulesEditorProps
) {
  const { t } = useTranslation()
  const [rules, setRules] = useState<Rule[]>(() =>
    flattenRules(safeParseJson(props.value))
  )

  const { onChange } = props
  const emitChange = useCallback(
    (newRules: Rule[]) => {
      setRules(newRules)
      onChange(serializeRules(newRules))
    },
    [onChange]
  )

  const updateRule = useCallback(
    (id: string, field: 'targetGroup' | 'description', val: string) => {
      emitChange(rules.map((r) => (r._id === id ? { ...r, [field]: val } : r)))
    },
    [rules, emitChange]
  )

  const removeRule = useCallback(
    (id: string) => emitChange(rules.filter((r) => r._id !== id)),
    [rules, emitChange]
  )

  const removeGroup = useCallback(
    (groupName: string) =>
      emitChange(rules.filter((r) => r.userGroup !== groupName)),
    [rules, emitChange]
  )

  const addRuleToGroup = useCallback(
    (groupName: string) => {
      emitChange([
        ...rules,
        {
          _id: uid(),
          userGroup: groupName,
          targetGroup: '',
          description: '',
        },
      ])
    },
    [rules, emitChange]
  )

  const grouped = useMemo(() => {
    const map: Record<string, Rule[]> = {}
    const order: string[] = []
    for (const r of rules) {
      if (!r.userGroup) continue
      if (!map[r.userGroup]) {
        map[r.userGroup] = []
        order.push(r.userGroup)
      }
      map[r.userGroup].push(r)
    }
    return order.map((name) => ({ name, items: map[name] }))
  }, [rules])

  return (
    <Card className={sectionCardClassName}>
      <CardHeader className={sectionHeaderClassName}>
        <CardTitle>{t('Special usable group rules')}</CardTitle>
        <CardDescription>
          {t(
            'Users of a group see only the groups granted by a rule. Groups without a rule are hidden.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className='space-y-3'>
          {grouped.length === 0 ? (
            <EmptyState
              className='min-h-40'
              title={t('No rules yet. Add a group below to get started.')}
            />
          ) : (
            grouped.map((group) => (
              <GroupSection
                key={group.name}
                groupName={group.name}
                items={group.items}
                groupOptions={props.groupOptions}
                onUpdate={updateRule}
                onRemove={removeRule}
                onAdd={addRuleToGroup}
                onRemoveGroup={removeGroup}
              />
            ))
          )}

          <div className='flex items-center justify-center pt-2'>
            <GroupSelect
              className='w-full sm:w-72'
              options={props.groupOptions.filter(
                (name) => !grouped.some((g) => g.name === name)
              )}
              value=''
              placeholder={t('Add rules for a user group')}
              onValueChange={addRuleToGroup}
            />
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
