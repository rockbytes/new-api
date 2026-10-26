import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';

import { checkDocLayout } from './check-doc-layout.mjs';

function createWorkspace() {
  const root = mkdtempSync(join(tmpdir(), 'doc-layout-'));
  for (const directory of [
    'doc/todo/planned',
    'doc/todo/in-progress',
    'doc/todo/blocked',
    'doc/done',
  ]) {
    mkdirSync(join(root, directory), { recursive: true });
  }
  writeFileSync(join(root, 'doc/README.md'), '# Documentation\n');
  return root;
}

function writePlan(root, directory, content) {
  writeFileSync(join(root, 'doc', directory, 'example-plan.md'), content);
}

test('accepts a valid planned document', () => {
  const root = createWorkspace();
  try {
    writePlan(
      root,
      'todo/planned',
      `---
title: Example Plan
status: planned
created: 2026-08-12
---

## Acceptance Criteria

- The planned behavior is documented.
`,
    );

    assert.deepEqual(checkDocLayout(root), []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('accepts the standalone documentation repository root', () => {
  const root = mkdtempSync(join(tmpdir(), 'doc-repository-'));
  try {
    for (const directory of ['todo/planned', 'todo/in-progress', 'todo/blocked', 'done']) {
      mkdirSync(join(root, directory), { recursive: true });
    }
    writeFileSync(join(root, 'README.md'), '# Documentation\n');
    writeFileSync(join(root, '.gitattributes'), '* text=auto\n');

    assert.deepEqual(checkDocLayout(root), []);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('rejects a document whose status does not match its directory', () => {
  const root = createWorkspace();
  try {
    writePlan(
      root,
      'todo/planned',
      `---
title: Example Plan
status: in-progress
created: 2026-08-12
---
`,
    );

    assert.match(checkDocLayout(root).join('\n'), /status.*planned/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('requires completion metadata and verification for done documents', () => {
  const root = createWorkspace();
  try {
    writePlan(
      root,
      'done',
      `---
title: Example Plan
status: done
created: 2026-08-12
---

## Acceptance Criteria

- The implementation is complete.
`,
    );

    const errors = checkDocLayout(root).join('\n');
    assert.match(errors, /completed/);
    assert.match(errors, /验证结果|verification/i);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('requires a blocker description for blocked documents', () => {
  const root = createWorkspace();
  try {
    writePlan(
      root,
      'todo/blocked',
      `---
title: Example Plan
status: blocked
created: 2026-08-12
---

## Acceptance Criteria

- The implementation is complete.
`,
    );

    assert.match(checkDocLayout(root).join('\n'), /阻塞原因|blockers/i);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('rejects the legacy finish directory', () => {
  const root = createWorkspace();
  try {
    mkdirSync(join(root, 'doc/finish'));

    const errors = checkDocLayout(root).join('\n');
    assert.match(errors, /doc[\\/]finish/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
