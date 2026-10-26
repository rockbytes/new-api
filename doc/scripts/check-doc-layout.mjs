import {
  existsSync,
  lstatSync,
  readFileSync,
  readdirSync,
} from 'node:fs';
import { extname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const TODO_STATUSES = ['planned', 'in-progress', 'blocked'];
const ALLOWED_DOC_ENTRIES = new Set([
  '.git',
  '.gitattributes',
  'README.md',
  'AGENTS.md',
  'package.json',
  'package-lock.json',
  'node_modules',
  'scripts',
  'todo',
  'done',
  'sdwan-architecture-layer.png',
  'sdwan-architecture-layer.svg',
]);
const REQUIRED_FIELDS = ['title', 'status', 'created'];

function displayPath(rootDir, filePath) {
  return relative(rootDir, filePath) || filePath;
}

function isDirectory(path) {
  return existsSync(path) && lstatSync(path).isDirectory();
}

function parseFrontmatter(content) {
  const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
  if (!match) {
    return null;
  }

  const fields = {};
  for (const line of match[1].split(/\r?\n/)) {
    const separator = line.indexOf(':');
    if (separator < 0) {
      continue;
    }

    const key = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    if (key) {
      fields[key] = value;
    }
  }
  return fields;
}

function hasNonEmptySection(content, names) {
  const heading = new RegExp(
    `^#{1,6}\\s+(?:${names.join('|')})[ \\t]*$`,
    'im',
  ).exec(content);
  if (!heading) {
    return false;
  }

  const sectionStart = heading.index + heading[0].length;
  const remaining = content.slice(sectionStart);
  const nextHeading = /^#{1,6}\s+/m.exec(remaining);
  const section = nextHeading
    ? remaining.slice(0, nextHeading.index)
    : remaining;
  return section.trim().length > 0;
}

function validatePlanFile(rootDir, filePath, status, errors) {
  const relativeFilePath = displayPath(rootDir, filePath);
  const content = readFileSync(filePath, 'utf8');
  const fields = parseFrontmatter(content);

  if (!fields) {
    errors.push(`${relativeFilePath}: missing valid YAML frontmatter`);
    return;
  }

  for (const field of REQUIRED_FIELDS) {
    if (!fields[field]) {
      errors.push(`${relativeFilePath}: missing required field '${field}'`);
    }
  }

  if (fields.status && fields.status !== status) {
    errors.push(
      `${relativeFilePath}: status '${fields.status}' does not match directory '${status}'`,
    );
  }

  if (status === 'blocked' && !hasNonEmptySection(content, ['阻塞原因', 'Blockers'])) {
    errors.push(`${relativeFilePath}: blocked documents require a non-empty 阻塞原因 or Blockers section`);
  }

  if (status === 'done') {
    if (!fields.completed) {
      errors.push(`${relativeFilePath}: done documents require a 'completed' field`);
    }
    if (!hasNonEmptySection(content, ['验证结果', 'Verification'])) {
      errors.push(`${relativeFilePath}: done documents require a non-empty 验证结果 or Verification section`);
    }
  }
}

function validatePlanDirectory(rootDir, directory, status, errors) {
  if (!isDirectory(directory)) {
    errors.push(`${displayPath(rootDir, directory)}: required directory is missing`);
    return;
  }

  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (entry.name === '.gitkeep') {
      continue;
    }

    const entryPath = join(directory, entry.name);
    if (entry.isDirectory()) {
      errors.push(`${displayPath(rootDir, entryPath)}: nested directories are not allowed`);
      continue;
    }

    if (extname(entry.name).toLowerCase() !== '.md') {
      errors.push(`${displayPath(rootDir, entryPath)}: plan files must use the .md extension`);
      continue;
    }

    validatePlanFile(rootDir, entryPath, status, errors);
  }
}

export function checkDocLayout(rootDir = process.cwd()) {
  const errors = [];
  const standaloneDocRepository =
    isDirectory(join(rootDir, 'todo')) && isDirectory(join(rootDir, 'done'));
  const docDirectory = standaloneDocRepository ? rootDir : join(rootDir, 'doc');
  const displayRoot = standaloneDocRepository ? resolve(rootDir, '..') : rootDir;

  if (!isDirectory(docDirectory)) {
    return ['doc: required directory is missing'];
  }

  for (const entry of readdirSync(docDirectory, { withFileTypes: true })) {
    if (!ALLOWED_DOC_ENTRIES.has(entry.name)) {
      errors.push(`${displayPath(rootDir, join(docDirectory, entry.name))}: unexpected entry`);
    }
  }

  const readmePath = join(docDirectory, 'README.md');
  if (!existsSync(readmePath) || !lstatSync(readmePath).isFile()) {
    errors.push(`${displayPath(displayRoot, readmePath)}: required file is missing`);
  }

  const todoDirectory = join(docDirectory, 'todo');
  if (!isDirectory(todoDirectory)) {
    errors.push(`${displayPath(displayRoot, todoDirectory)}: required directory is missing`);
  } else {
    for (const entry of readdirSync(todoDirectory, { withFileTypes: true })) {
      if (!TODO_STATUSES.includes(entry.name) || !entry.isDirectory()) {
        errors.push(`${displayPath(displayRoot, join(todoDirectory, entry.name))}: unexpected todo entry`);
      }
    }
    for (const status of TODO_STATUSES) {
      validatePlanDirectory(displayRoot, join(todoDirectory, status), status, errors);
    }
  }

  validatePlanDirectory(displayRoot, join(docDirectory, 'done'), 'done', errors);

  return errors;
}

const currentFile = process.argv[1] ? resolve(process.argv[1]) : '';
if (currentFile === fileURLToPath(import.meta.url)) {
  const errors = checkDocLayout();
  if (errors.length > 0) {
    console.error('Documentation layout check failed:');
    for (const error of errors) {
      console.error(`- ${error}`);
    }
    process.exitCode = 1;
  } else {
    console.log('Documentation layout check passed.');
  }
}
