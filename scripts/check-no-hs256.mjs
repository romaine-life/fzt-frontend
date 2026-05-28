#!/usr/bin/env node
// Migration guard for the auth.romaine.life RS256 cutover.
// Per tank-operator/docs/migration-policy.md: the legacy HS256
// (api-jwt-signing-secret) verification path is deleted end-to-end and must
// not creep back. This backend now verifies auth.romaine.life RS256 tokens
// against the issuer JWKS via `jose` only. If any HS256 code pattern
// reappears in source, fail CI so the reintroduction is caught at PR time.
//
// Note: matches the *code* surface (jsonwebtoken, jwt.verify, the jwtSecret
// plumbing), not the secret name string — so explanatory comments that
// mention the retired secret by name do not trip the guard. `jose` and
// jwtVerify (no dot) are the sanctioned surface and are intentionally allowed.

import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

const ignoredDirs = new Set([
  '.claude', '.git', '.terraform', 'build', 'coverage', 'dist', 'node_modules',
]);
const ignoredFiles = new Set(['package-lock.json', 'pnpm-lock.yaml', 'yarn.lock']);
const ignoredRelativePaths = new Set(['scripts/check-no-hs256.mjs']);

const blocked = [
  { name: 'jsonwebtoken dep / use', pattern: /\bjsonwebtoken\b/ },
  { name: 'HS256 jwt.sign / jwt.verify', pattern: /\bjwt\.(sign|verify)\b/ },
  { name: 'jwtSigningSecret config plumbing', pattern: /\bjwtSigningSecret\b/ },
  { name: 'createRequireAuth jwtSecret param', pattern: /createRequireAuth\s*\(\s*\{\s*jwtSecret/ },
];

const failures = [];
for await (const filePath of walk(repoRoot)) {
  const relativePath = toRepoPath(filePath);
  if (ignoredRelativePaths.has(relativePath)) continue;
  const bytes = await fs.readFile(filePath);
  if (bytes.includes(0)) continue;
  const text = bytes.toString('utf8');
  for (const rule of blocked) {
    const match = rule.pattern.exec(text);
    if (!match) continue;
    const { line, column } = lineAndColumn(text, match.index);
    failures.push(`${relativePath}:${line}:${column} ${rule.name}: ${JSON.stringify(match[0])}`);
  }
}

if (failures.length > 0) {
  console.error('Retired HS256 auth surface detected:');
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}
console.log('No retired HS256 auth surfaces found.');

async function* walk(dir) {
  const entries = await fs.readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    const absolutePath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!ignoredDirs.has(entry.name)) yield* walk(absolutePath);
      continue;
    }
    if (!entry.isFile() || ignoredFiles.has(entry.name)) continue;
    yield absolutePath;
  }
}
function toRepoPath(filePath) {
  return path.relative(repoRoot, filePath).split(path.sep).join('/');
}
function lineAndColumn(text, index) {
  const before = text.slice(0, index);
  const lines = before.split(/\r\n|\r|\n/);
  return { line: lines.length, column: lines[lines.length - 1].length + 1 };
}
